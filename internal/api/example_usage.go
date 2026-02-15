package api

import (
	"database/sql"
	"log/slog"

	"github.com/go-redis/redis/v8"
	clientv3 "go.etcd.io/etcd/client/v3"
)

// Example of how to use the server with health checks and graceful shutdown
func ExampleUsage(logger *slog.Logger, db *sql.DB, redisClient *redis.Client, etcdClient *clientv3.Client) {
	// Create server with all dependencies
	_ = Config{
		Address:    ":8080",
		EnableAuth: true,
		EnableCORS: true,
	}

	// Create server (assumes runtime, scheduler, scaler, quotaManager, jwtManager exist)
	// server := New(logger, config, runtime, scheduler, scaler, quotaManager, jwtManager)

	// Register health checks for dependencies
	// server.RegisterHealthCheck("database", DatabaseHealthCheck(db))
	// server.RegisterHealthCheck("redis", RedisHealthCheck(redisClient))
	// server.RegisterHealthCheck("etcd", EtcdHealthCheck(etcdClient))

	// Register custom shutdown hooks if needed
	// server.ShutdownManager().RegisterHook("custom_cleanup", 50, func(ctx context.Context) error {
	//     logger.InfoContext(ctx, "running custom cleanup")
	//     // Custom cleanup logic here
	//     return nil
	// })

	// Start server
	// if err := server.Start(context.Background()); err != nil {
	//     logger.Error("failed to start server", "error", err)
	// }

	// Wait for shutdown signal
	// server.ShutdownManager().Wait()
}
