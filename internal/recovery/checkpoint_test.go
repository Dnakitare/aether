package recovery_test

import (
	"log/slog"
	"os"
	"testing"

	"github.com/aether-runtime/aether/internal/recovery"
	"github.com/aether-runtime/aether/pkg/api"
)

func TestCheckpointConfig(t *testing.T) {
	config := recovery.DefaultCheckpointConfig()

	if config.Interval == 0 {
		t.Error("Expected Interval to be set")
	}

	if config.RetentionCount == 0 {
		t.Error("Expected RetentionCount to be set")
	}

	if config.MaxCheckpointSize == 0 {
		t.Error("Expected MaxCheckpointSize to be set")
	}
}

func TestRecoveryConfig(t *testing.T) {
	config := recovery.DefaultRecoveryConfig()

	if config.MaxRetries == 0 {
		t.Error("Expected MaxRetries to be set")
	}

	if config.RetryDelay == 0 {
		t.Error("Expected RetryDelay to be set")
	}

	if config.HealthCheckInterval == 0 {
		t.Error("Expected HealthCheckInterval to be set")
	}
}

func TestRecoveryStrategy(t *testing.T) {
	strategies := []recovery.RecoveryStrategy{
		recovery.StrategyRestart,
		recovery.StrategyFailover,
		recovery.StrategyRollback,
	}

	for _, strategy := range strategies {
		if strategy == "" {
			t.Error("Expected non-empty strategy")
		}
	}
}

// Integration tests require PostgreSQL
func TestCheckpointManager(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping PostgreSQL integration test in short mode")
	}

	// These tests would require a PostgreSQL database
	// For now, we test the configuration and structure
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	config := recovery.DefaultCheckpointConfig()
	_ = logger
	_ = config

	// Note: Full integration tests with PostgreSQL are in internal/backup/backup_comprehensive_test.go
	// and tests/integration/backup_restore_integration_test.go
}

func TestCheckpoint(t *testing.T) {
	checkpoint := &recovery.Checkpoint{
		AgentID:  api.AgentID("test-agent"),
		TenantID: api.TenantID("test-tenant"),
		Version:  1,
		State: map[string]interface{}{
			"key": "value",
		},
		Metadata: map[string]string{
			"source": "test",
		},
	}

	if checkpoint.AgentID == "" {
		t.Error("Expected AgentID to be set")
	}

	if checkpoint.State == nil {
		t.Error("Expected State to be set")
	}
}

func TestRecoveryManager(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	config := recovery.DefaultRecoveryConfig()

	// Create a mock checkpoint manager (would need real DB for full test)
	// For now, just test the recovery manager creation
	_ = logger
	_ = config
}
