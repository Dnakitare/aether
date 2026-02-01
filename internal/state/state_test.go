package state_test

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/dnakitare/aether/internal/state"
	"github.com/dnakitare/aether/pkg/api"
)

// TestRedisStore tests Redis store operations.
// This test requires a running Redis instance.
func TestRedisStore(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping Redis integration test in short mode")
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	config := state.Config{
		Address:    "localhost:6379",
		KeyPrefix:  "test:",
		DefaultTTL: 1 * time.Hour,
	}

	store, err := state.NewRedisStore(logger, config)
	if err != nil {
		t.Skipf("Redis not available: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Test health check
	if err := store.Health(ctx); err != nil {
		t.Fatalf("Health check failed: %v", err)
	}

	// Test agent state
	t.Run("AgentState", func(t *testing.T) {
		agentInfo := &api.AgentInfo{
			Config: api.AgentConfig{
				ID:       api.AgentID("test-agent-1"),
				TenantID: api.TenantID("tenant-1"),
				Name:     "Test Agent",
				Image:    "python:3.11",
			},
			Status:    api.AgentStatusRunning,
			CreatedAt: time.Now(),
		}

		// Save agent state
		if err := store.SaveAgentState(ctx, agentInfo); err != nil {
			t.Fatalf("Failed to save agent state: %v", err)
		}

		// Get agent state
		retrieved, err := store.GetAgentState(ctx, agentInfo.Config.ID)
		if err != nil {
			t.Fatalf("Failed to get agent state: %v", err)
		}

		if retrieved.Config.ID != agentInfo.Config.ID {
			t.Errorf("AgentID = %s, want %s", retrieved.Config.ID, agentInfo.Config.ID)
		}

		// List agents by tenant
		agents, err := store.ListAgentsByTenant(ctx, agentInfo.Config.TenantID)
		if err != nil {
			t.Fatalf("Failed to list agents: %v", err)
		}

		if len(agents) == 0 {
			t.Error("Expected at least one agent")
		}

		// Delete agent state
		if err := store.DeleteAgentState(ctx, agentInfo.Config.ID, agentInfo.Config.TenantID); err != nil {
			t.Fatalf("Failed to delete agent state: %v", err)
		}
	})

	// Test distributed locking
	t.Run("DistributedLock", func(t *testing.T) {
		lockName := "test-lock"

		// Acquire lock
		acquired, err := store.Lock(ctx, lockName, 10*time.Second)
		if err != nil {
			t.Fatalf("Failed to acquire lock: %v", err)
		}
		if !acquired {
			t.Error("Expected to acquire lock")
		}

		// Try to acquire again (should fail)
		acquired, err = store.Lock(ctx, lockName, 10*time.Second)
		if err != nil {
			t.Fatalf("Failed to try lock: %v", err)
		}
		if acquired {
			t.Error("Should not acquire lock twice")
		}

		// Release lock
		if err := store.Unlock(ctx, lockName); err != nil {
			t.Fatalf("Failed to release lock: %v", err)
		}

		// Acquire again (should succeed)
		acquired, err = store.Lock(ctx, lockName, 10*time.Second)
		if err != nil {
			t.Fatalf("Failed to reacquire lock: %v", err)
		}
		if !acquired {
			t.Error("Expected to reacquire lock after release")
		}

		// Cleanup
		store.Unlock(ctx, lockName)
	})

	// Test sessions
	t.Run("Session", func(t *testing.T) {
		sessionID := "test-session-123"
		sessionData := map[string]interface{}{
			"user_id":   "user-1",
			"tenant_id": "tenant-1",
			"role":      "admin",
		}

		// Set session
		if err := store.SetSession(ctx, sessionID, sessionData, 1*time.Hour); err != nil {
			t.Fatalf("Failed to set session: %v", err)
		}

		// Get session
		retrieved, err := store.GetSession(ctx, sessionID)
		if err != nil {
			t.Fatalf("Failed to get session: %v", err)
		}

		if retrieved["user_id"] != "user-1" {
			t.Errorf("UserID = %v, want user-1", retrieved["user_id"])
		}

		// Delete session
		if err := store.DeleteSession(ctx, sessionID); err != nil {
			t.Fatalf("Failed to delete session: %v", err)
		}

		// Get deleted session (should fail)
		_, err = store.GetSession(ctx, sessionID)
		if err == nil {
			t.Error("Expected error for deleted session")
		}
	})

	// Test counters
	t.Run("Counter", func(t *testing.T) {
		counterName := "test-counter"

		// Increment counter
		count, err := store.IncrementCounter(ctx, counterName)
		if err != nil {
			t.Fatalf("Failed to increment counter: %v", err)
		}
		if count != 1 {
			t.Errorf("Count = %d, want 1", count)
		}

		// Increment again
		count, err = store.IncrementCounter(ctx, counterName)
		if err != nil {
			t.Fatalf("Failed to increment counter: %v", err)
		}
		if count != 2 {
			t.Errorf("Count = %d, want 2", count)
		}

		// Get counter
		count, err = store.GetCounter(ctx, counterName)
		if err != nil {
			t.Fatalf("Failed to get counter: %v", err)
		}
		if count != 2 {
			t.Errorf("Count = %d, want 2", count)
		}

		// Cleanup
		store.Delete(ctx, "counter:"+counterName)
	})
}
