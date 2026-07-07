package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	goredis "github.com/redis/go-redis/v9"
	"github.com/spf13/cobra"

	apiserver "github.com/dnakitare/aether/internal/api"
	"github.com/dnakitare/aether/internal/audit"
	"github.com/dnakitare/aether/internal/auth"
	"github.com/dnakitare/aether/internal/config"
	"github.com/dnakitare/aether/internal/database"
	"github.com/dnakitare/aether/internal/ratelimit"
	"github.com/dnakitare/aether/internal/runtime"
	"github.com/dnakitare/aether/internal/runtime/vm"
	"github.com/dnakitare/aether/internal/scaler"
	"github.com/dnakitare/aether/internal/scheduler"
	"github.com/dnakitare/aether/internal/state"
	"github.com/dnakitare/aether/internal/tenant"
	"github.com/dnakitare/aether/migrations"
	"github.com/dnakitare/aether/pkg/api"
)

var serverCmd = &cobra.Command{
	Use:   "server",
	Short: "Start the Aether API server",
	Long: `Start the Aether API server.

Runs a single-instance, single-region control plane: in-process scheduler,
PostgreSQL state, and the HTTP API.`,
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
		"address", cfg.Server.Address,
	)

	// Initialize PostgreSQL state store.
	// DATABASE_URL takes priority; fall back to the structured config block.
	postgresURL := os.Getenv("DATABASE_URL")
	if postgresURL == "" {
		postgresURL = fmt.Sprintf(
			"postgres://%s:%s@%s:%d/%s?sslmode=%s",
			cfg.Database.User,
			cfg.Database.Password,
			cfg.Database.Host,
			cfg.Database.Port,
			cfg.Database.Database,
			cfg.Database.SSLMode,
		)
	}

	pgConfig := state.PostgresConfig{
		DSN:             postgresURL,
		MaxOpenConns:    cfg.Database.MaxOpenConns,
		MaxIdleConns:    cfg.Database.MaxIdleConns,
		ConnMaxLifetime: cfg.Database.ConnMaxLifetime,
	}

	stateStore, err := state.NewPostgresStore(logger, pgConfig)
	if err != nil {
		return fmt.Errorf("failed to create postgres store: %w", err)
	}
	defer func() {
		if err := stateStore.Close(); err != nil {
			logger.Error("failed to close state store", "error", err)
		}
	}()

	logger.InfoContext(ctx, "postgres store initialized")

	// Initialize audit logger. Failure is fatal unless AUDIT_DISABLED=true is
	// set (development only). Audit records are a compliance requirement; silently
	// missing them would be a security gap.
	var auditLogger *audit.Logger
	if al, aerr := audit.NewLogger(logger, audit.Config{DSN: postgresURL}); aerr != nil {
		if os.Getenv("AUDIT_DISABLED") != "true" {
			return fmt.Errorf("audit logger unavailable: %w", aerr)
		}
		logger.WarnContext(ctx, "AUDIT_DISABLED=true: audit events will not be recorded (not for production)", "error", aerr)
	} else {
		auditLogger = al
	}

	// Run database migrations automatically on startup.
	migConfig := database.MigrationConfig{
		MigrationsFS: migrations.FS,
		DatabaseName: cfg.Database.Database,
	}
	if err := database.RunMigrations(logger, stateStore.DB(), migConfig); err != nil {
		return fmt.Errorf("database migration failed: %w", err)
	}

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
	if rtConfig.DefaultResources.CPUCount <= 0 || rtConfig.DefaultResources.MemoryMB <= 0 {
		return fmt.Errorf("DefaultResources must have CPUCount > 0 and MemoryMB > 0")
	}

	rt, err := runtime.New(logger, rtConfig, stateStore)
	if err != nil {
		return fmt.Errorf("failed to create runtime: %w", err)
	}
	defer func() {
		if err := rt.Shutdown(ctx); err != nil {
			logger.Error("failed to shutdown runtime", "error", err)
		}
	}()

	// Reconcile in-memory state from the database so ListAgents works after restart.
	// Store failed agents so we can release their quota after quotaManager is initialized.
	failedAgents, err := rt.Reconcile(ctx)
	if err != nil {
		logger.WarnContext(ctx, "runtime reconciliation failed, agent listing may be incomplete", "error", err)
	}

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

	// Register the local node so RecordAllocation has a target. In a
	// distributed deployment each instance would register itself here.
	sched.RegisterNode(ctx, &scheduler.Node{
		ID:   scheduler.LocalNodeID,
		Name: "local",
		Capacity: scheduler.Resources{
			CPUCores: 8000,   // 8 cores in millicores
			MemoryMB: 16384,  // 16 GB
			DiskMB:   102400, // 100 GB
		},
		Agents: make(map[api.AgentID]*scheduler.AgentAllocation),
	})

	logger.InfoContext(ctx, "scheduler initialized")

	// Initialize JWT Manager. JWT_SECRET env var takes priority over config file.
	const defaultJWTSecret = "aether-alpha-secret-change-in-production"
	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		jwtSecret = cfg.Security.JWTSecretKey
	}
	if jwtSecret == "" {
		jwtSecret = defaultJWTSecret
		logger.WarnContext(ctx, "using default JWT secret - set JWT_SECRET env var for production")
	}
	if cfg.Server.EnableAuth && jwtSecret == defaultJWTSecret {
		return fmt.Errorf("authentication is enabled but JWT_SECRET is not set; " +
			"refusing to start with the default insecure secret")
	}

	jwtConfig := auth.Config{
		SecretKey:      jwtSecret,
		PrivateKeyPath: cfg.Security.JWTPrivateKeyPath,
		PublicKeyPath:  cfg.Security.JWTPublicKeyPath,
		TokenDuration:  24 * time.Hour,
		Issuer:         "aether-alpha",
	}

	jwtManager, err := auth.NewJWTManager(jwtConfig)
	if err != nil {
		return fmt.Errorf("failed to create JWT manager: %w", err)
	}

	if cfg.Security.JWTPrivateKeyPath != "" {
		logger.InfoContext(ctx, "JWT manager initialized with RS256 asymmetric signing")
	} else {
		logger.InfoContext(ctx, "JWT manager initialized with HS256 symmetric signing")
	}

	// Initialize Quota Manager backed by Postgres for persistence across restarts.
	quotaStore := tenant.NewPostgresQuotaStore(stateStore.DB())
	quotaManager := tenant.NewQuotaManager(logger, quotaStore)
	if err := quotaManager.LoadFromStore(ctx); err != nil {
		logger.WarnContext(ctx, "failed to load quotas from database; starting with empty quota state", "error", err)
	}

	// Release quota for any agents that failed to restart during reconciliation.
	for _, info := range failedAgents {
		resources := tenant.ResourceRequest{
			AgentCount: 1,
			CPUCores:   int64(info.Config.Resources.CPUCount) * 1000,
			MemoryMB:   info.Config.Resources.MemoryMB,
			DiskMB:     info.Config.Resources.DiskMB,
		}
		if rerr := quotaManager.ReleaseResources(ctx, info.Config.TenantID, resources); rerr != nil {
			logger.WarnContext(ctx, "failed to release quota for failed agent on reconcile",
				"agent_id", info.Config.ID, "error", rerr)
		}
	}

	// Initialize auto-scaler with runtime adapters.
	scalerCfg := scaler.Config{
		Interval:        30 * time.Second,
		DefaultCooldown: 5 * time.Minute,
	}
	metricsProvider := scaler.NewRuntimeMetricsProvider(rt)
	scaleExecutor := scaler.NewRuntimeScaleExecutor(rt)
	sc := scaler.New(logger, scalerCfg, metricsProvider, scaleExecutor)
	go sc.Start(ctx)
	defer sc.Stop()

	// Create HTTP API Server
	serverAddr := os.Getenv("SERVER_ADDRESS")
	if serverAddr == "" {
		serverAddr = ":8080"
	}

	apiConfig := apiserver.Config{
		Address:        serverAddr,
		ReadTimeout:    15 * time.Second,
		WriteTimeout:   15 * time.Second,
		EnableCORS:     cfg.Server.EnableCORS,
		AllowedOrigins: cfg.Server.AllowedOrigins,
		EnableAuth:     cfg.Server.EnableAuth,
	}

	if !apiConfig.EnableAuth {
		logger.WarnContext(ctx, "AUTHENTICATION IS DISABLED - do not use in production",
			"enable_auth", false,
		)
	}

	apiSrv := apiserver.New(logger, apiConfig, rt, sched, sc, quotaManager, jwtManager)

	// Wire metrics into the runtime so agent create/destroy/error operations
	// are recorded as Prometheus counters.
	rt.SetMetrics(apiSrv.Metrics())

	if auditLogger != nil {
		apiSrv.WithAuditLogger(auditLogger)
	}
	apiSrv.WithTokenIssuer(apiserver.NewPostgresTokenIssuer(stateStore.DB(), jwtManager, logger))

	// Wire rate limiter
	rlRedis := goredis.NewClient(&goredis.Options{
		Addr:     cfg.Redis.Address,
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.DB,
	})
	tb := ratelimit.NewTokenBucket(logger, rlRedis, ratelimit.Config{})
	ml := ratelimit.NewMultiLayerLimiter(logger, tb)
	apiSrv.WithRateLimiter(ratelimit.Middleware(logger, ml))

	// Register dependency health checks for the readiness probe.
	apiSrv.RegisterHealthCheck("postgres", apiserver.DatabaseHealthCheck(stateStore.DB()))
	apiSrv.RegisterHealthCheck("redis", func(hctx context.Context) error {
		hctx, cancel := context.WithTimeout(hctx, 2*time.Second)
		defer cancel()
		if err := rlRedis.Ping(hctx).Err(); err != nil {
			return fmt.Errorf("redis ping failed: %w", err)
		}
		return nil
	})

	// Start API server
	if err := apiSrv.Start(ctx); err != nil {
		return fmt.Errorf("failed to start API server: %w", err)
	}
	defer func() {
		if err := apiSrv.Stop(ctx); err != nil {
			logger.Error("failed to stop API server", "error", err)
		}
	}()

	logger.InfoContext(ctx, "Aether server started successfully")

	// Handle shutdown signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	logger.InfoContext(ctx, "press Ctrl+C to stop")

	// Wait for shutdown signal
	<-sigCh

	logger.InfoContext(ctx, "shutting down server")

	// Deferred Stop calls (API server, scheduler, DB) run as this function
	// returns and handle their own teardown.
	logger.InfoContext(ctx, "server stopped")
	return nil
}
