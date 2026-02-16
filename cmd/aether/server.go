package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	apiserver "github.com/aether-runtime/aether/internal/api"
	"github.com/aether-runtime/aether/internal/auth"
	"github.com/aether-runtime/aether/internal/config"
	"github.com/aether-runtime/aether/internal/runtime"
	"github.com/aether-runtime/aether/internal/runtime/vm"
	"github.com/aether-runtime/aether/internal/scaler"
	"github.com/aether-runtime/aether/internal/scheduler"
	"github.com/aether-runtime/aether/internal/scheduler/distributed"
	"github.com/aether-runtime/aether/internal/state"
	"github.com/aether-runtime/aether/internal/tenant"
	"github.com/aether-runtime/aether/pkg/api"
	"github.com/spf13/cobra"
)

var serverCmd = &cobra.Command{
	Use:   "server",
	Short: "Start the Aether API server",
	Long: `Start the Aether API server with distributed scheduler support.

The server can run in two modes:
  - local:       Single-instance in-memory scheduler
  - distributed: Multi-instance distributed scheduler with etcd, Kafka, and Redis

Mode is configured via config file or environment variables.`,
	RunE: runServer,
}

func init() {
	rootCmd.AddCommand(serverCmd)
}

func runServer(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	// Load configuration
	cfg, err := config.Load(configFile)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Override logger with config settings
	logger = cfg.GetLogger()
	slog.SetDefault(logger)

	logger.InfoContext(ctx, "starting Aether server",
		"mode", cfg.Scheduler.Mode,
		"address", cfg.Server.Address,
	)

	// Initialize distributed scheduler components if in distributed mode
	var (
		shardManager *distributed.ShardManager
		nodeRegistry *distributed.NodeRegistry
		queue        distributed.Queue
	)

	if cfg.Scheduler.Mode == "distributed" {
		logger.InfoContext(ctx, "initializing distributed scheduler components",
			"scheduler_id", cfg.Scheduler.SchedulerID,
			"instance_id", cfg.Scheduler.InstanceID,
		)

		// Create shard manager
		shardConfig := distributed.ShardConfig{
			EtcdEndpoints:     cfg.Etcd.Endpoints,
			KeyPrefix:         cfg.Etcd.KeyPrefix,
			SchedulerID:       cfg.Scheduler.SchedulerID,
			InstanceID:        cfg.Scheduler.InstanceID,
			Hostname:          cfg.Scheduler.Hostname,
			SessionTTL:        cfg.Etcd.SessionTTL,
			HeartbeatInterval: cfg.Scheduler.HeartbeatInterval,
			VirtualNodes:      cfg.Scheduler.VirtualNodes,
		}

		shardManager, err = distributed.NewShardManager(logger, shardConfig)
		if err != nil {
			return fmt.Errorf("failed to create shard manager: %w", err)
		}
		defer shardManager.Stop(ctx)

		if err := shardManager.Start(ctx); err != nil {
			return fmt.Errorf("failed to start shard manager: %w", err)
		}

		logger.InfoContext(ctx, "shard manager started")

		// Create node registry
		nodeRegistryConfig := distributed.NodeRegistryConfig{
			RedisAddr:     cfg.Redis.Address,
			RedisPassword: cfg.Redis.Password,
			RedisDB:       cfg.Redis.DB,
			KeyPrefix:     "aether",
			NodeTTL:       cfg.Redis.NodeTTL,
		}

		nodeRegistry, err = distributed.NewNodeRegistry(logger, nodeRegistryConfig)
		if err != nil {
			return fmt.Errorf("failed to create node registry: %w", err)
		}
		defer nodeRegistry.Close()

		logger.InfoContext(ctx, "node registry initialized")

		// Create distributed queue (with automatic fallback to in-memory if Kafka unavailable)
		queueConfig := distributed.QueueConfig{
			Brokers:           cfg.Kafka.Brokers,
			Topic:             cfg.Kafka.Topic,
			DLQTopic:          cfg.Kafka.DLQTopic,
			ConsumerGroup:     cfg.Kafka.ConsumerGroup,
			NumWorkers:        cfg.Scheduler.NumWorkers,
			MaxRetries:        cfg.Kafka.MaxRetries,
			RequestTimeout:    cfg.Kafka.RequestTimeout,
			PartitionStrategy: cfg.Kafka.PartitionStrategy,
		}

		queue, err = distributed.NewQueue(logger, queueConfig)
		if err != nil {
			return fmt.Errorf("failed to create queue: %w", err)
		}
		defer queue.Stop(ctx)

		// Register placement handler
		// TODO: Wire up actual placement logic from runtime
		queue.RegisterHandler(func(ctx context.Context, req *distributed.SchedulingRequest) error {
			logger.InfoContext(ctx, "received scheduling request",
				"agent_id", req.AgentID,
				"tenant_id", req.TenantID,
			)
			// Placeholder - actual implementation will use nodeRegistry.TryAllocate
			return fmt.Errorf("not yet implemented")
		})

		if err := queue.Start(ctx); err != nil {
			return fmt.Errorf("failed to start distributed queue: %w", err)
		}

		logger.InfoContext(ctx, "distributed queue started")
	}

	// Initialize PostgreSQL state store (Alpha: use environment variable or default)
	postgresURL := os.Getenv("DATABASE_URL")
	if postgresURL == "" {
		postgresURL = "postgres://postgres:postgres@localhost:5432/aether?sslmode=disable"
	}

	pgConfig := state.PostgresConfig{
		DSN:             postgresURL,
		MaxOpenConns:    25,
		MaxIdleConns:    5,
		ConnMaxLifetime: 5 * time.Minute,
	}

	stateStore, err := state.NewPostgresStore(logger, pgConfig)
	if err != nil {
		return fmt.Errorf("failed to create postgres store: %w", err)
	}
	defer stateStore.Close()

	logger.InfoContext(ctx, "postgres store initialized")

	// Initialize Runtime (Alpha: use sensible defaults)
	rtConfig := runtime.Config{
		VMManagerConfig: vm.ManagerConfig{
			FirecrackerBinary: "/usr/local/bin/firecracker",
			WorkspaceDir:      "/tmp/aether/vms",
			KernelImage:       "/var/lib/aether/vmlinux",
			RootFSImage:       "/var/lib/aether/rootfs.ext4",
		},
		DefaultResources: api.ResourceLimits{
			CPUCount: 1,
			MemoryMB: 512,
		},
		WorkspaceDir: "/tmp/aether",
	}

	rt, err := runtime.New(logger, rtConfig, stateStore)
	if err != nil {
		return fmt.Errorf("failed to create runtime: %w", err)
	}
	defer rt.Shutdown(ctx)

	logger.InfoContext(ctx, "runtime initialized")

	// Initialize Scheduler (local mode for Alpha)
	schedConfig := scheduler.Config{
		Strategy:        scheduler.BinPacking,
		Interval:        1 * time.Second,
		EventBufferSize: 100,
	}

	sched := scheduler.New(logger, schedConfig)
	go sched.Start(ctx)
	defer sched.Stop()

	// Register a default node for Alpha (single-node)
	sched.RegisterNode(ctx, &scheduler.Node{
		ID:   "alpha-node-1",
		Name: "Alpha Node 1",
		Capacity: scheduler.Resources{
			CPUCores: 8000,  // 8 cores in millicores
			MemoryMB: 16384, // 16GB
			DiskMB:   102400,
		},
		Agents: make(map[api.AgentID]*scheduler.AgentAllocation),
	})

	logger.InfoContext(ctx, "scheduler initialized")

	// Initialize JWT Manager (Alpha: use environment variable or default)
	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		jwtSecret = "aether-alpha-secret-change-in-production"
		logger.WarnContext(ctx, "using default JWT secret - set JWT_SECRET env var for production")
	}

	jwtConfig := auth.Config{
		SecretKey:     jwtSecret,
		TokenDuration: 24 * time.Hour,
		Issuer:        "aether-alpha",
	}

	jwtManager, err := auth.NewJWTManager(jwtConfig)
	if err != nil {
		return fmt.Errorf("failed to create JWT manager: %w", err)
	}

	logger.InfoContext(ctx, "JWT manager initialized")

	// Initialize Quota Manager
	quotaManager := tenant.NewQuotaManager(logger)

	// Scaler not needed for Alpha (requires additional setup)
	var sc *scaler.Scaler = nil

	// Create HTTP API Server
	serverAddr := os.Getenv("SERVER_ADDRESS")
	if serverAddr == "" {
		serverAddr = ":8080"
	}

	apiConfig := apiserver.Config{
		Address:      serverAddr,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		EnableCORS:   false,
		EnableAuth:   false, // Disabled for Alpha to simplify testing
	}

	apiSrv := apiserver.New(logger, apiConfig, rt, sched, sc, quotaManager, jwtManager)

	// Start API server
	if err := apiSrv.Start(ctx); err != nil {
		return fmt.Errorf("failed to start API server: %w", err)
	}
	defer apiSrv.Stop(ctx)

	logger.InfoContext(ctx, "Aether server started successfully")
	logger.InfoContext(ctx, "scheduler mode", "mode", cfg.Scheduler.Mode)
	if cfg.Scheduler.Mode == "distributed" {
		logger.InfoContext(ctx, "distributed components ready",
			"scheduler_id", cfg.Scheduler.SchedulerID,
			"etcd", cfg.Etcd.Endpoints,
			"kafka", cfg.Kafka.Brokers,
			"redis", cfg.Redis.Address,
		)
	}

	// Handle shutdown signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	logger.InfoContext(ctx, "press Ctrl+C to stop")

	// Wait for shutdown signal
	<-sigCh

	logger.InfoContext(ctx, "shutting down server")

	// Graceful shutdown with timeout
	shutdownCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// Stop distributed components
	if cfg.Scheduler.Mode == "distributed" {
		if queue != nil {
			logger.InfoContext(shutdownCtx, "stopping distributed queue")
			if err := queue.Stop(shutdownCtx); err != nil {
				logger.ErrorContext(shutdownCtx, "error stopping queue", "error", err)
			}
		}

		if nodeRegistry != nil {
			logger.InfoContext(shutdownCtx, "closing node registry")
			if err := nodeRegistry.Close(); err != nil {
				logger.ErrorContext(shutdownCtx, "error closing node registry", "error", err)
			}
		}

		if shardManager != nil {
			logger.InfoContext(shutdownCtx, "stopping shard manager")
			if err := shardManager.Stop(shutdownCtx); err != nil {
				logger.ErrorContext(shutdownCtx, "error stopping shard manager", "error", err)
			}
		}
	}

	logger.InfoContext(ctx, "server stopped")
	return nil
}
