package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/go-redis/redis/v8"
	clientv3 "go.etcd.io/etcd/client/v3"
)

// HealthChecker provides health check functionality for the API server.
type HealthChecker struct {
	logger *slog.Logger
	checks map[string]HealthCheck
	mu     sync.RWMutex
}

// HealthCheck is a function that performs a health check.
type HealthCheck func(ctx context.Context) error

// HealthStatus represents the overall health status.
type HealthStatus struct {
	Status    string                 `json:"status"` // "healthy", "degraded", "unhealthy"
	Timestamp time.Time              `json:"timestamp"`
	Checks    map[string]CheckResult `json:"checks"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

// CheckResult represents the result of an individual health check.
type CheckResult struct {
	Status    string        `json:"status"` // "pass", "warn", "fail"
	Duration  time.Duration `json:"duration"`
	Error     string        `json:"error,omitempty"`
	Timestamp time.Time     `json:"timestamp"`
}

// NewHealthChecker creates a new health checker.
func NewHealthChecker(logger *slog.Logger) *HealthChecker {
	return &HealthChecker{
		logger: logger,
		checks: make(map[string]HealthCheck),
	}
}

// RegisterCheck registers a health check with a given name.
func (hc *HealthChecker) RegisterCheck(name string, check HealthCheck) {
	hc.mu.Lock()
	defer hc.mu.Unlock()
	hc.checks[name] = check
}

// Health returns the overall health status.
// This endpoint is suitable for liveness probes (does the service respond?).
func (hc *HealthChecker) Health(w http.ResponseWriter, r *http.Request) {
	// Simple liveness check - just respond with 200 if the service is running
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	response := map[string]string{
		"status": "ok",
		"time":   time.Now().Format(time.RFC3339),
	}

	json.NewEncoder(w).Encode(response)
}

// Readiness returns detailed readiness status including dependency checks.
// This endpoint is suitable for readiness probes (can the service handle traffic?).
func (hc *HealthChecker) Readiness(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	hc.mu.RLock()
	checks := make(map[string]HealthCheck, len(hc.checks))
	for name, check := range hc.checks {
		checks[name] = check
	}
	hc.mu.RUnlock()

	// Run all checks in parallel
	results := make(map[string]CheckResult)
	var wg sync.WaitGroup
	var mu sync.Mutex

	for name, check := range checks {
		wg.Add(1)
		go func(name string, check HealthCheck) {
			defer wg.Done()

			start := time.Now()
			err := check(ctx)
			duration := time.Since(start)

			result := CheckResult{
				Duration:  duration,
				Timestamp: time.Now(),
			}

			if err != nil {
				result.Status = "fail"
				result.Error = err.Error()
			} else {
				result.Status = "pass"
			}

			mu.Lock()
			results[name] = result
			mu.Unlock()
		}(name, check)
	}

	wg.Wait()

	// Determine overall status
	status := "healthy"
	failCount := 0
	warnCount := 0

	for _, result := range results {
		if result.Status == "fail" {
			failCount++
		} else if result.Status == "warn" {
			warnCount++
		}
	}

	if failCount > 0 {
		status = "unhealthy"
	} else if warnCount > 0 {
		status = "degraded"
	}

	healthStatus := HealthStatus{
		Status:    status,
		Timestamp: time.Now(),
		Checks:    results,
	}

	// Set HTTP status code based on overall status
	statusCode := http.StatusOK
	if status == "unhealthy" {
		statusCode = http.StatusServiceUnavailable
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(healthStatus)
}

// DatabaseHealthCheck creates a health check for database connectivity.
func DatabaseHealthCheck(db *sql.DB) HealthCheck {
	return func(ctx context.Context) error {
		// Use a short timeout for ping
		ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()

		if err := db.PingContext(ctx); err != nil {
			return fmt.Errorf("database ping failed: %w", err)
		}

		// Check if we can execute a simple query
		var result int
		if err := db.QueryRowContext(ctx, "SELECT 1").Scan(&result); err != nil {
			return fmt.Errorf("database query failed: %w", err)
		}

		return nil
	}
}

// RedisHealthCheck creates a health check for Redis connectivity.
func RedisHealthCheck(client *redis.Client) HealthCheck {
	return func(ctx context.Context) error {
		ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()

		if err := client.Ping(ctx).Err(); err != nil {
			return fmt.Errorf("redis ping failed: %w", err)
		}

		return nil
	}
}

// EtcdHealthCheck creates a health check for etcd connectivity.
func EtcdHealthCheck(client *clientv3.Client) HealthCheck {
	return func(ctx context.Context) error {
		ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()

		// Check etcd cluster health
		_, err := client.Get(ctx, "health-check", clientv3.WithLimit(1))
		if err != nil {
			return fmt.Errorf("etcd connectivity failed: %w", err)
		}

		return nil
	}
}

// KafkaHealthCheck creates a health check for Kafka connectivity.
// Pass in your Kafka admin client or producer to check connectivity.
func KafkaHealthCheck(kafkaClient KafkaHealthChecker) HealthCheck {
	return func(ctx context.Context) error {
		if kafkaClient == nil {
			// Kafka not configured, skip check
			return nil
		}
		return kafkaClient.Ping(ctx)
	}
}

// KafkaHealthChecker is an interface for checking Kafka health.
// Implement this interface with your Kafka client (e.g., Shopify/sarama, confluent-kafka-go).
type KafkaHealthChecker interface {
	Ping(ctx context.Context) error
}

// Example implementation for Shopify/sarama:
//
// type SaramaHealthChecker struct {
//     client sarama.Client
// }
//
// func (s *SaramaHealthChecker) Ping(ctx context.Context) error {
//     brokers := s.client.Brokers()
//     if len(brokers) == 0 {
//         return fmt.Errorf("no kafka brokers available")
//     }
//
//     // Check if at least one broker is connected
//     for _, broker := range brokers {
//         if broker.Connected() {
//             return nil
//         }
//     }
//     return fmt.Errorf("no kafka brokers connected")
// }

// CustomHealthCheck creates a health check from a custom function.
func CustomHealthCheck(name string, checkFn func(ctx context.Context) error) HealthCheck {
	return checkFn
}
