package chaos

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"testing"
	"time"

	_ "github.com/lib/pq"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	clientv3 "go.etcd.io/etcd/client/v3"

	"github.com/aether-runtime/aether/internal/auth"
	"github.com/aether-runtime/aether/internal/backup"
	"github.com/aether-runtime/aether/internal/ha"
	"github.com/aether-runtime/aether/internal/scheduler"
	"github.com/aether-runtime/aether/internal/state"
	pkgapi "github.com/aether-runtime/aether/pkg/api"
)

// ChaosEnvironment holds components for chaos testing
type ChaosEnvironment struct {
	T *testing.T

	// Infrastructure
	DB          *sql.DB
	RedisClient *redis.Client
	EtcdClient  *clientv3.Client
	Logger      *slog.Logger

	// Core components
	Scheduler  *scheduler.Scheduler
	StateStore *state.RedisStore
	Auth       *auth.JWTManager
	Backup     *backup.BackupManager
	HA         *ha.LeaderElection

	// Test data
	TenantID  pkgapi.TenantID
	UserID    string
	AuthToken string

	// Cleanup functions
	Cleanup []func()
}

// SetupChaosEnvironment creates a test environment for chaos testing
func SetupChaosEnvironment(t *testing.T) *ChaosEnvironment {
	t.Helper()

	env := &ChaosEnvironment{
		T:        t,
		TenantID: pkgapi.TenantID("chaos-tenant"),
		UserID:   "chaos-user",
		Cleanup:  []func(){},
	}

	// Setup logger
	env.Logger = slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	// Setup infrastructure
	env.setupInfrastructure()

	// Setup core components
	env.setupAuth()
	env.setupScheduler()

	// Setup backup if database available
	if env.DB != nil {
		env.setupBackup()
	}

	// Setup HA if etcd available
	if env.EtcdClient != nil {
		env.setupHA()
	}

	return env
}

// setupInfrastructure connects to test infrastructure
func (env *ChaosEnvironment) setupInfrastructure() {
	// Try to connect to PostgreSQL
	db, err := sql.Open("postgres", "postgres://aether:aether_test_password@localhost:5433/aether_test?sslmode=disable")
	if err == nil {
		if err := db.Ping(); err == nil {
			env.DB = db
			env.Cleanup = append(env.Cleanup, func() {
				db.Close()
			})
			env.T.Log("Connected to PostgreSQL")
		}
	}

	// Try to connect to Redis
	redisClient := redis.NewClient(&redis.Options{
		Addr:     "localhost:6380",
		Password: "redis_test_password",
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := redisClient.Ping(ctx).Err(); err == nil {
		env.RedisClient = redisClient
		env.Cleanup = append(env.Cleanup, func() {
			redisClient.Close()
		})
		env.T.Log("Connected to Redis")

		// Setup state store
		stateConfig := state.Config{
			Address:   "localhost:6380",
			Password:  "redis_test_password",
			DB:        0,
			KeyPrefix: "chaos:",
		}
		stateStore, err := state.NewRedisStore(env.Logger, stateConfig)
		if err == nil {
			env.StateStore = stateStore
		}
	}

	// Try to connect to etcd
	etcdClient, err := clientv3.New(clientv3.Config{
		Endpoints:   []string{"localhost:2379"},
		DialTimeout: 2 * time.Second,
	})
	if err == nil {
		env.EtcdClient = etcdClient
		env.Cleanup = append(env.Cleanup, func() {
			etcdClient.Close()
		})
		env.T.Log("Connected to etcd")
	}
}

// setupAuth creates JWT manager
func (env *ChaosEnvironment) setupAuth() {
	config := auth.Config{
		SecretKey:     "chaos-test-secret-key-12345",
		TokenDuration: 1 * time.Hour,
		Issuer:        "aether-chaos-test",
	}

	jwtManager, err := auth.NewJWTManager(config)
	require.NoError(env.T, err)
	env.Auth = jwtManager

	// Generate test token
	token, err := env.Auth.GenerateToken(env.TenantID, env.UserID, "admin")
	require.NoError(env.T, err)
	env.AuthToken = token
}

// setupScheduler creates a scheduler instance
func (env *ChaosEnvironment) setupScheduler() {
	config := scheduler.Config{
		Strategy:        scheduler.BinPacking,
		Interval:        1 * time.Second,
		EventBufferSize: 100,
	}

	env.Scheduler = scheduler.New(env.Logger, config)

	// Add test nodes
	nodes := []*scheduler.Node{
		{
			ID:   "chaos-node-1",
			Name: "Chaos Node 1",
			Labels: map[string]string{
				"zone": "chaos-zone",
			},
			Capacity: scheduler.Resources{
				CPUCores: 8000,
				MemoryMB: 16384,
				DiskMB:   102400,
			},
			Agents: make(map[pkgapi.AgentID]*scheduler.AgentAllocation),
		},
	}

	ctx := context.Background()
	for _, node := range nodes {
		env.Scheduler.RegisterNode(ctx, node)
	}

	go env.Scheduler.Start(ctx)

	env.Cleanup = append(env.Cleanup, func() {
		// Use recover to handle potential double-stop
		defer func() {
			if r := recover(); r != nil {
				// Ignore panic from closing already-closed channel
				env.T.Logf("Scheduler stop recovered from panic (likely already stopped): %v", r)
			}
		}()
		env.Scheduler.Stop()
	})
}

// setupBackup creates backup manager
func (env *ChaosEnvironment) setupBackup() {
	if env.DB == nil || env.RedisClient == nil {
		env.T.Log("PostgreSQL or Redis unavailable, skipping backup setup")
		return
	}

	tempDir := env.T.TempDir()

	backupConfig := backup.BackupConfig{
		BackupDir:     tempDir,
		RetentionDays: 7,
		Compression:   true,
	}

	env.Backup = backup.NewBackupManager(env.Logger, backupConfig, env.DB, env.RedisClient)
}

// setupHA creates HA leader election
func (env *ChaosEnvironment) setupHA() {
	if env.EtcdClient == nil {
		env.T.Log("etcd unavailable, skipping HA setup")
		return
	}

	haConfig := ha.DefaultElectionConfig()
	haConfig.LeaderName = "chaos-leader"
	haConfig.SessionTTL = 5

	election, err := ha.NewLeaderElection(env.Logger, haConfig)
	if err != nil {
		env.T.Logf("Failed to create HA election: %v", err)
		return
	}

	env.HA = election
	env.Cleanup = append(env.Cleanup, func() {
		election.Close()
	})
}

// TearDown cleans up all test resources
func (env *ChaosEnvironment) TearDown() {
	for i := len(env.Cleanup) - 1; i >= 0; i-- {
		env.Cleanup[i]()
	}
}

// CreateTestAgent creates an agent configuration for testing
func (env *ChaosEnvironment) CreateTestAgent(name string) *pkgapi.AgentConfig {
	return &pkgapi.AgentConfig{
		ID:       pkgapi.AgentID(fmt.Sprintf("agent-%s-%d", name, time.Now().UnixNano())),
		TenantID: env.TenantID,
		Name:     name,
		Image:    "test-image:latest",
		Resources: pkgapi.ResourceLimits{
			CPUCount: 2,
			MemoryMB: 2048,
			DiskMB:   10240,
		},
	}
}

// SkipIfNoInfrastructure skips test if required infrastructure is unavailable
func (env *ChaosEnvironment) SkipIfNoInfrastructure(t *testing.T, services ...string) {
	t.Helper()
	for _, service := range services {
		switch service {
		case "postgres", "postgresql":
			if env.DB == nil {
				t.Skip("PostgreSQL not available for chaos test")
			}
		case "redis":
			if env.RedisClient == nil {
				t.Skip("Redis not available for chaos test")
			}
		case "etcd":
			if env.EtcdClient == nil {
				t.Skip("etcd not available for chaos test")
			}
		}
	}
}

// WaitForCondition polls until condition is true or timeout
func WaitForCondition(t *testing.T, condition func() bool, timeout time.Duration, interval time.Duration, description string) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(interval)
	}

	t.Fatalf("Timeout waiting for condition: %s", description)
}

// AssertEventually checks condition becomes true within timeout
func AssertEventually(t *testing.T, condition func() bool, timeout time.Duration, msgAndArgs ...interface{}) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	for {
		if condition() {
			return
		}

		<-ticker.C
		if time.Now().After(deadline) {
			msg := "Condition never became true"
			if len(msgAndArgs) > 0 {
				msg = fmt.Sprintf(msgAndArgs[0].(string), msgAndArgs[1:]...)
			}
			t.Fatal(msg)
		}
	}
}

// SimulateRedisFailure closes Redis connection to simulate failure
func (env *ChaosEnvironment) SimulateRedisFailure() error {
	if env.RedisClient == nil {
		return fmt.Errorf("redis client not initialized")
	}

	env.T.Log("Simulating Redis failure")
	return env.RedisClient.Close()
}

// RestoreRedis reconnects to Redis
func (env *ChaosEnvironment) RestoreRedis() error {
	env.T.Log("Restoring Redis connection")

	redisClient := redis.NewClient(&redis.Options{
		Addr:     "localhost:6380",
		Password: "redis_test_password",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := redisClient.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("failed to restore Redis: %w", err)
	}

	env.RedisClient = redisClient

	// Recreate state store
	stateConfig := state.Config{
		Address:   "localhost:6380",
		Password:  "redis_test_password",
		DB:        0,
		KeyPrefix: "chaos:",
	}
	stateStore, err := state.NewRedisStore(env.Logger, stateConfig)
	if err != nil {
		return fmt.Errorf("failed to create state store: %w", err)
	}
	env.StateStore = stateStore

	return nil
}

// SimulatePostgresFailure closes database connection
func (env *ChaosEnvironment) SimulatePostgresFailure() error {
	if env.DB == nil {
		return fmt.Errorf("database not initialized")
	}

	env.T.Log("Simulating PostgreSQL failure")
	return env.DB.Close()
}

// RestorePostgres reconnects to PostgreSQL
func (env *ChaosEnvironment) RestorePostgres() error {
	env.T.Log("Restoring PostgreSQL connection")

	db, err := sql.Open("postgres", "postgres://aether:aether_test_password@localhost:5433/aether_test?sslmode=disable")
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}

	if err := db.Ping(); err != nil {
		return fmt.Errorf("failed to ping database: %w", err)
	}

	env.DB = db
	return nil
}

// SimulateEtcdFailure closes etcd connection
func (env *ChaosEnvironment) SimulateEtcdFailure() error {
	if env.EtcdClient == nil {
		return fmt.Errorf("etcd client not initialized")
	}

	env.T.Log("Simulating etcd failure")
	return env.EtcdClient.Close()
}

// RestoreEtcd reconnects to etcd
func (env *ChaosEnvironment) RestoreEtcd() error {
	env.T.Log("Restoring etcd connection")

	etcdClient, err := clientv3.New(clientv3.Config{
		Endpoints:   []string{"localhost:2379"},
		DialTimeout: 5 * time.Second,
	})
	if err != nil {
		return fmt.Errorf("failed to create etcd client: %w", err)
	}

	env.EtcdClient = etcdClient
	return nil
}
