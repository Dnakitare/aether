package integration

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	_ "github.com/lib/pq"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/aether-runtime/aether/internal/api"
	"github.com/aether-runtime/aether/internal/auth"
	"github.com/aether-runtime/aether/internal/backup"
	"github.com/aether-runtime/aether/internal/ha"
	"github.com/aether-runtime/aether/internal/ratelimit"
	"github.com/aether-runtime/aether/internal/runtime"
	"github.com/aether-runtime/aether/internal/scheduler"
	pkgapi "github.com/aether-runtime/aether/pkg/api"
)

// TestEnvironment holds all components for integration testing
type TestEnvironment struct {
	T *testing.T

	// Infrastructure
	DB          *sql.DB
	RedisClient *redis.Client
	Logger      *slog.Logger

	// Core components
	Runtime   *runtime.Runtime
	Scheduler *scheduler.Scheduler
	Auth      *auth.JWTManager
	APIKeys   *auth.APIKeyManager
	RateLimit *ratelimit.MultiLayerLimiter
	Backup    *backup.BackupManager
	Restore   *backup.RestoreManager
	HA        *ha.LeaderElection

	// API server
	Server     *api.Server
	TestServer *httptest.Server

	// Test data
	TenantID  pkgapi.TenantID
	UserID    string
	AuthToken string
	APIKey    string

	// Cleanup functions
	Cleanup []func()
}

// SetupTestEnvironment creates a complete test environment with all components
func SetupTestEnvironment(t *testing.T) *TestEnvironment {
	t.Helper()

	env := &TestEnvironment{
		T:        t,
		TenantID: pkgapi.TenantID("test-tenant"),
		UserID:   "test-user",
		Cleanup:  []func(){},
	}

	// Setup logger
	env.Logger = slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	// Setup infrastructure (conditionally)
	env.setupInfrastructure()

	// Setup core components
	env.setupAuth()
	env.setupScheduler()
	env.setupRateLimit()

	// Setup backup/restore if database available
	if env.DB != nil {
		env.setupBackup()
	}

	// Setup API server
	env.setupAPIServer()

	return env
}

// setupInfrastructure connects to test infrastructure (PostgreSQL, Redis, etcd)
func (env *TestEnvironment) setupInfrastructure() {
	// Try to connect to PostgreSQL
	db, err := sql.Open("postgres", "postgres://postgres:postgres@localhost:5432/aether_test?sslmode=disable")
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
		Addr: "localhost:6379",
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := redisClient.Ping(ctx).Err(); err == nil {
		env.RedisClient = redisClient
		env.Cleanup = append(env.Cleanup, func() {
			redisClient.Close()
		})
		env.T.Log("Connected to Redis")
	}
}

// setupAuth creates JWT manager and generates test token
func (env *TestEnvironment) setupAuth() {
	config := auth.Config{
		SecretKey:     "test-secret-key-integration-12345",
		TokenDuration: 1 * time.Hour,
		Issuer:        "aether-integration-test",
	}

	jwtManager, err := auth.NewJWTManager(config)
	require.NoError(env.T, err)
	env.Auth = jwtManager

	// Generate test token
	token, err := env.Auth.GenerateToken(env.TenantID, env.UserID, "admin")
	require.NoError(env.T, err)
	env.AuthToken = token

	// Setup API key manager
	env.APIKeys = auth.NewAPIKeyManager(env.Logger)

	// Create test API key
	apiKey, _, err := env.APIKeys.CreateKey(
		context.Background(),
		env.TenantID,
		"Integration Test Key",
		[]auth.Permission{
			auth.PermissionAgentCreate,
			auth.PermissionAgentRead,
			auth.PermissionAgentUpdate,
			auth.PermissionAgentDelete,
		},
		1*time.Hour,
	)
	require.NoError(env.T, err)
	env.APIKey = apiKey
}

// setupScheduler creates a scheduler instance
func (env *TestEnvironment) setupScheduler() {
	config := scheduler.Config{
		Strategy:        scheduler.BinPacking,
		Interval:        1 * time.Second,
		EventBufferSize: 100,
	}

	env.Scheduler = scheduler.New(env.Logger, config)

	// Add test nodes
	nodes := []*scheduler.Node{
		{
			ID:   "node-1",
			Name: "Test Node 1",
			Labels: map[string]string{
				"zone": "us-east-1a",
			},
			Capacity: scheduler.Resources{
				CPUCores: 8000, // 8 cores in millicores
				MemoryMB: 16384,
				DiskMB:   102400,
			},
			Agents: make(map[pkgapi.AgentID]*scheduler.AgentAllocation),
		},
		{
			ID:   "node-2",
			Name: "Test Node 2",
			Labels: map[string]string{
				"zone": "us-east-1b",
			},
			Capacity: scheduler.Resources{
				CPUCores: 8000, // 8 cores in millicores
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

	// Start scheduler
	go env.Scheduler.Start(ctx)

	env.Cleanup = append(env.Cleanup, func() {
		env.Scheduler.Stop()
	})
}

// setupRateLimit creates rate limiter (uses in-memory if Redis unavailable)
func (env *TestEnvironment) setupRateLimit() {
	var redisClient *redis.Client
	if env.RedisClient != nil {
		redisClient = env.RedisClient
	} else {
		// Use miniredis for in-memory testing
		env.T.Log("Redis unavailable, rate limiter will use miniredis in actual tests")
		return
	}

	config := ratelimit.Config{
		DefaultRate:  100,
		DefaultBurst: 200,
		KeyPrefix:    "integration:",
	}

	if redisClient != nil {
		tokenBucket := ratelimit.NewTokenBucket(env.Logger, redisClient, config)
		env.RateLimit = ratelimit.NewMultiLayerLimiter(env.Logger, tokenBucket)
	}
}

// setupBackup creates backup/restore managers
func (env *TestEnvironment) setupBackup() {
	if env.DB == nil {
		env.T.Log("PostgreSQL unavailable, skipping backup/restore setup")
		return
	}

	tempDir := env.T.TempDir()

	backupConfig := backup.BackupConfig{
		BackupDir:     tempDir,
		RetentionDays: 7,
		Compression:   true,
	}

	// For integration tests, we need a Redis client (use miniredis if real Redis unavailable)
	redisClient := env.RedisClient
	if redisClient == nil {
		env.T.Log("Redis unavailable for backup tests")
		return
	}

	env.Backup = backup.NewBackupManager(env.Logger, backupConfig, env.DB, redisClient)

	restoreConfig := backup.RestoreConfig{
		BackupDir: tempDir,
	}

	env.Restore = backup.NewRestoreManager(env.Logger, restoreConfig, env.DB, redisClient)
}

// setupAPIServer creates HTTP API server for testing
func (env *TestEnvironment) setupAPIServer() {
	// For now, create a minimal test server
	// In a real implementation, this would initialize the full API server
	mux := http.NewServeMux()

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"healthy"}`))
	})

	env.TestServer = httptest.NewServer(mux)

	env.Cleanup = append(env.Cleanup, func() {
		env.TestServer.Close()
	})
}

// TearDown cleans up all test resources
func (env *TestEnvironment) TearDown() {
	for i := len(env.Cleanup) - 1; i >= 0; i-- {
		env.Cleanup[i]()
	}
}

// CreateTestAgent helper to create an agent configuration
func (env *TestEnvironment) CreateTestAgent(name string) *pkgapi.AgentConfig {
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

		select {
		case <-ticker.C:
			if time.Now().After(deadline) {
				msg := "Condition never became true"
				if len(msgAndArgs) > 0 {
					msg = fmt.Sprintf(msgAndArgs[0].(string), msgAndArgs[1:]...)
				}
				t.Fatal(msg)
			}
		}
	}
}

// HTTPRequest makes an authenticated HTTP request to test server
func (env *TestEnvironment) HTTPRequest(method, path string, body interface{}) (*http.Response, error) {
	url := env.TestServer.URL + path

	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		return nil, err
	}

	// Add auth header
	req.Header.Set("Authorization", "Bearer "+env.AuthToken)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 5 * time.Second}
	return client.Do(req)
}

// SkipIfNoInfrastructure skips test if required infrastructure is unavailable
func (env *TestEnvironment) SkipIfNoInfrastructure(services ...string) {
	for _, service := range services {
		switch service {
		case "postgres", "postgresql":
			if env.DB == nil {
				env.T.Skip("PostgreSQL not available for integration test")
			}
		case "redis":
			if env.RedisClient == nil {
				env.T.Skip("Redis not available for integration test")
			}
		case "etcd":
			if env.HA == nil {
				env.T.Skip("etcd not available for integration test")
			}
		}
	}
}
