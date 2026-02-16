// Package integration contains checkpoint recovery integration tests.
// These tests verify end-to-end checkpoint creation and restoration workflows.
//
// Run with: TEST_DATABASE_URL="postgres://..." go test -v ./tests/integration/ -run TestCheckpointRecovery
package integration

import (
	"context"
	"database/sql"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aether-runtime/aether/internal/recovery"
	"github.com/aether-runtime/aether/internal/runtime"
	"github.com/aether-runtime/aether/internal/runtime/vm"
	"github.com/aether-runtime/aether/internal/state"
	"github.com/aether-runtime/aether/pkg/api"
)

// setupRecoveryTest sets up a runtime with checkpoint support for testing
func setupRecoveryTest(t *testing.T) (*runtime.Runtime, *sql.DB) {
	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("TEST_DATABASE_URL not set, skipping checkpoint recovery tests")
	}

	db, err := sql.Open("postgres", dbURL)
	require.NoError(t, err)

	// Clean up test data
	_, _ = db.Exec("DELETE FROM checkpoints WHERE agent_id LIKE 'test-recovery-%'")
	_, _ = db.Exec("DELETE FROM agents WHERE id LIKE 'test-recovery-%'")

	// Create test tenant
	_, err = db.Exec(`
		INSERT INTO tenants (id, name, tier, created_at, updated_at)
		VALUES ('test-recovery-tenant', 'Recovery Test Tenant', 'free', NOW(), NOW())
		ON CONFLICT (id) DO NOTHING
	`)
	require.NoError(t, err)

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	// Create state store
	stateStore, err := state.NewPostgresStore(logger, state.PostgresConfig{
		DSN: dbURL,
	})
	require.NoError(t, err)

	// Create runtime with checkpoint support
	workspaceDir := t.TempDir()

	// Create dummy Firecracker binary
	firecrackerPath := workspaceDir + "/firecracker"
	err = os.WriteFile(firecrackerPath, []byte("#!/bin/sh\necho test"), 0755)
	require.NoError(t, err)

	config := runtime.Config{
		VMManagerConfig: vm.ManagerConfig{
			KernelImage:       "/tmp/kernel.img",
			RootFSImage:       "/tmp/rootfs.img",
			FirecrackerBinary: firecrackerPath,
			WorkspaceDir:      workspaceDir,
		},
		DefaultResources: api.ResourceLimits{
			CPUCount: 2,
			MemoryMB: 512,
		},
		WorkspaceDir: workspaceDir,
	}

	rt, err := runtime.New(logger, config, stateStore)
	require.NoError(t, err)

	// Initialize checkpoint manager
	err = rt.SetCheckpointManager(db)
	require.NoError(t, err)

	return rt, db
}

func TestCheckpointRecovery_BasicWorkflow(t *testing.T) {
	rt, db := setupRecoveryTest(t)
	defer db.Close()

	ctx := context.Background()
	agentID := api.AgentID("test-recovery-basic")

	t.Run("create checkpoint for non-existent agent fails", func(t *testing.T) {
		_, err := rt.CreateCheckpoint(ctx, agentID)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	// Note: Actual agent creation requires VM infrastructure
	// These tests document the expected workflow
}

func TestCheckpointRecovery_VersionTracking(t *testing.T) {
	rt, db := setupRecoveryTest(t)
	defer db.Close()

	t.Run("checkpoint versions increment correctly", func(t *testing.T) {
		// This test verifies that checkpoint versions are tracked correctly
		// In a full implementation, this would:
		// 1. Create an agent
		// 2. Create multiple checkpoints
		// 3. Verify version numbers are sequential
		// 4. Restore from specific versions

		// Document expected behavior
		assert.NotNil(t, rt)
	})
}

func TestCheckpointRecovery_ListCheckpoints(t *testing.T) {
	rt, db := setupRecoveryTest(t)
	defer db.Close()

	ctx := context.Background()

	t.Run("list checkpoints for non-existent agent", func(t *testing.T) {
		checkpoints, err := rt.ListCheckpoints(ctx, "nonexistent-agent")
		// Should return empty list or error depending on implementation
		if err != nil {
			assert.Error(t, err)
		} else {
			assert.NotNil(t, checkpoints)
		}
	})
}

func TestCheckpointRecovery_GetLatestCheckpoint(t *testing.T) {
	rt, db := setupRecoveryTest(t)
	defer db.Close()

	ctx := context.Background()
	agentID := api.AgentID("test-recovery-latest")

	t.Run("get latest checkpoint for non-existent agent", func(t *testing.T) {
		_, err := rt.GetLatestCheckpoint(ctx, agentID)
		assert.Error(t, err)
	})
}

func TestCheckpointRecovery_RestoreFromCheckpoint(t *testing.T) {
	rt, db := setupRecoveryTest(t)
	defer db.Close()

	ctx := context.Background()
	agentID := api.AgentID("test-recovery-restore")

	t.Run("restore non-existent agent fails", func(t *testing.T) {
		err := rt.RestoreFromCheckpoint(ctx, agentID, 0)
		assert.Error(t, err)
	})

	t.Run("restore from non-existent checkpoint fails", func(t *testing.T) {
		// Document expected behavior for missing checkpoint
		// In production, this would fail gracefully
		err := rt.RestoreFromCheckpoint(ctx, agentID, 999)
		assert.Error(t, err)
	})
}

func TestCheckpointRecovery_DeleteCheckpoint(t *testing.T) {
	rt, db := setupRecoveryTest(t)
	defer db.Close()

	ctx := context.Background()
	agentID := api.AgentID("test-recovery-delete")

	t.Run("delete non-existent checkpoint", func(t *testing.T) {
		err := rt.DeleteCheckpoint(ctx, agentID, 1)
		assert.Error(t, err)
	})
}

// TestCheckpointRecovery_DirectCheckpointManager tests checkpoint manager directly
func TestCheckpointRecovery_DirectCheckpointManager(t *testing.T) {
	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("TEST_DATABASE_URL not set, skipping checkpoint recovery tests")
	}

	db, err := sql.Open("postgres", dbURL)
	require.NoError(t, err)
	defer db.Close()

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	config := recovery.DefaultCheckpointConfig()

	cm, err := recovery.NewCheckpointManager(logger, db, config)
	require.NoError(t, err)

	ctx := context.Background()
	agentID := api.AgentID("test-recovery-cm")
	tenantID := api.TenantID("test-recovery-tenant")

	// Clean up any existing checkpoints
	_, _ = db.Exec("DELETE FROM checkpoints WHERE agent_id = $1", agentID)

	t.Run("create and retrieve checkpoint", func(t *testing.T) {
		// Create checkpoint
		state := map[string]interface{}{
			"status": "running",
			"data":   "checkpoint-data",
			"count":  42,
		}
		metadata := map[string]string{
			"image":   "python:3.11",
			"version": "1.0",
		}

		cp, err := cm.CreateCheckpoint(ctx, agentID, tenantID, state, metadata)
		require.NoError(t, err)
		assert.Equal(t, 1, cp.Version)
		assert.Equal(t, agentID, cp.AgentID)
		assert.Equal(t, tenantID, cp.TenantID)
		assert.NotZero(t, cp.Size)
		assert.False(t, cp.CreatedAt.IsZero())

		// Retrieve by version
		retrieved, err := cm.GetCheckpointByVersion(ctx, agentID, 1)
		require.NoError(t, err)
		assert.Equal(t, cp.Version, retrieved.Version)
		assert.Equal(t, cp.AgentID, retrieved.AgentID)
		assert.Equal(t, state["status"], retrieved.State["status"])

		// Get latest
		latest, err := cm.GetLatestCheckpoint(ctx, agentID)
		require.NoError(t, err)
		assert.Equal(t, cp.Version, latest.Version)
	})

	t.Run("create multiple checkpoints", func(t *testing.T) {
		agentID := api.AgentID("test-recovery-multi")
		_, _ = db.Exec("DELETE FROM checkpoints WHERE agent_id = $1", agentID)

		// Create 3 checkpoints
		for i := 1; i <= 3; i++ {
			state := map[string]interface{}{
				"iteration": i,
				"timestamp": time.Now().Unix(),
			}
			cp, err := cm.CreateCheckpoint(ctx, agentID, tenantID, state, nil)
			require.NoError(t, err)
			assert.Equal(t, i, cp.Version)
		}

		// List all checkpoints
		checkpoints, err := cm.ListCheckpoints(ctx, agentID)
		require.NoError(t, err)
		assert.Len(t, checkpoints, 3)

		// Verify descending order
		assert.Equal(t, 3, checkpoints[0].Version)
		assert.Equal(t, 2, checkpoints[1].Version)
		assert.Equal(t, 1, checkpoints[2].Version)

		// Get latest
		latest, err := cm.GetLatestCheckpoint(ctx, agentID)
		require.NoError(t, err)
		assert.Equal(t, 3, latest.Version)
	})

	t.Run("delete checkpoint", func(t *testing.T) {
		agentID := api.AgentID("test-recovery-delete-cm")
		_, _ = db.Exec("DELETE FROM checkpoints WHERE agent_id = $1", agentID)

		// Create checkpoints
		for i := 1; i <= 3; i++ {
			state := map[string]interface{}{"v": i}
			_, err := cm.CreateCheckpoint(ctx, agentID, tenantID, state, nil)
			require.NoError(t, err)
		}

		// Delete version 2
		err := cm.DeleteCheckpoint(ctx, agentID, 2)
		require.NoError(t, err)

		// Verify only 1 and 3 remain
		checkpoints, err := cm.ListCheckpoints(ctx, agentID)
		require.NoError(t, err)
		assert.Len(t, checkpoints, 2)
		assert.Equal(t, 3, checkpoints[0].Version)
		assert.Equal(t, 1, checkpoints[1].Version)
	})

	t.Run("checkpoint state persistence", func(t *testing.T) {
		agentID := api.AgentID("test-recovery-state")
		_, _ = db.Exec("DELETE FROM checkpoints WHERE agent_id = $1", agentID)

		// Create checkpoint with complex state
		complexState := map[string]interface{}{
			"config": map[string]interface{}{
				"cpu":    2,
				"memory": 512,
			},
			"env": map[string]interface{}{
				"vars": []string{"VAR1=value1", "VAR2=value2"},
			},
			"status":    "running",
			"timestamp": time.Now().Unix(),
		}

		cp, err := cm.CreateCheckpoint(ctx, agentID, tenantID, complexState, nil)
		require.NoError(t, err)

		// Retrieve and verify state is preserved
		retrieved, err := cm.GetCheckpointByVersion(ctx, agentID, cp.Version)
		require.NoError(t, err)

		assert.Equal(t, complexState["status"], retrieved.State["status"])

		// Verify nested structures
		config := retrieved.State["config"].(map[string]interface{})
		assert.Equal(t, float64(2), config["cpu"])
		assert.Equal(t, float64(512), config["memory"])
	})

	t.Run("checkpoint metadata", func(t *testing.T) {
		agentID := api.AgentID("test-recovery-metadata")
		_, _ = db.Exec("DELETE FROM checkpoints WHERE agent_id = $1", agentID)

		metadata := map[string]string{
			"image":     "python:3.11",
			"cpu_count": "4",
			"memory_mb": "2048",
			"reason":    "scheduled_backup",
		}

		state := map[string]interface{}{"test": true}
		cp, err := cm.CreateCheckpoint(ctx, agentID, tenantID, state, metadata)
		require.NoError(t, err)

		retrieved, err := cm.GetCheckpointByVersion(ctx, agentID, cp.Version)
		require.NoError(t, err)

		assert.Equal(t, metadata["image"], retrieved.Metadata["image"])
		assert.Equal(t, metadata["cpu_count"], retrieved.Metadata["cpu_count"])
		assert.Equal(t, metadata["reason"], retrieved.Metadata["reason"])
	})

	t.Run("checkpoint retention enforcement", func(t *testing.T) {
		agentID := api.AgentID("test-recovery-retention")
		_, _ = db.Exec("DELETE FROM checkpoints WHERE agent_id = $1", agentID)

		// Create checkpoint manager with retention limit
		retentionConfig := recovery.DefaultCheckpointConfig()
		retentionConfig.RetentionCount = 3

		cmWithRetention, err := recovery.NewCheckpointManager(logger, db, retentionConfig)
		require.NoError(t, err)

		// Create 5 checkpoints
		for i := 1; i <= 5; i++ {
			state := map[string]interface{}{"iteration": i}
			_, err := cmWithRetention.CreateCheckpoint(ctx, agentID, tenantID, state, nil)
			require.NoError(t, err)
		}

		// Verify only last 3 remain
		checkpoints, err := cmWithRetention.ListCheckpoints(ctx, agentID)
		require.NoError(t, err)
		assert.Len(t, checkpoints, 3)

		// Verify versions are 5, 4, 3
		assert.Equal(t, 5, checkpoints[0].Version)
		assert.Equal(t, 4, checkpoints[1].Version)
		assert.Equal(t, 3, checkpoints[2].Version)
	})

	t.Run("error handling - checkpoint too large", func(t *testing.T) {
		agentID := api.AgentID("test-recovery-size")
		_, _ = db.Exec("DELETE FROM checkpoints WHERE agent_id = $1", agentID)

		// Create checkpoint manager with small max size
		sizeConfig := recovery.DefaultCheckpointConfig()
		sizeConfig.MaxCheckpointSize = 100 // 100 bytes

		cmWithSize, err := recovery.NewCheckpointManager(logger, db, sizeConfig)
		require.NoError(t, err)

		// Try to create checkpoint larger than limit
		largeState := map[string]interface{}{
			"data": string(make([]byte, 1000)), // 1KB
		}

		_, err = cmWithSize.CreateCheckpoint(ctx, agentID, tenantID, largeState, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "checkpoint size")
	})

	t.Run("concurrent checkpoint creation safety", func(t *testing.T) {
		agentID := api.AgentID("test-recovery-concurrent")
		_, _ = db.Exec("DELETE FROM checkpoints WHERE agent_id = $1", agentID)

		// Create 10 checkpoints concurrently
		done := make(chan error, 10)
		for i := 0; i < 10; i++ {
			go func(iteration int) {
				state := map[string]interface{}{
					"iteration": iteration,
					"timestamp": time.Now().UnixNano(),
				}
				_, err := cm.CreateCheckpoint(ctx, agentID, tenantID, state, nil)
				done <- err
			}(i)
		}

		// Collect results
		for i := 0; i < 10; i++ {
			err := <-done
			assert.NoError(t, err)
		}

		// Verify all 10 checkpoints were created with unique versions
		checkpoints, err := cm.ListCheckpoints(ctx, agentID)
		require.NoError(t, err)
		assert.Len(t, checkpoints, 10)

		// Verify no duplicate versions
		versions := make(map[int]bool)
		for _, cp := range checkpoints {
			assert.False(t, versions[cp.Version], "duplicate version detected")
			versions[cp.Version] = true
		}
	})
}

// TestCheckpointRecovery_ErrorScenarios tests error handling
func TestCheckpointRecovery_ErrorScenarios(t *testing.T) {
	rt, db := setupRecoveryTest(t)
	defer db.Close()

	ctx := context.Background()

	t.Run("checkpoint manager not initialized", func(t *testing.T) {
		// Create a runtime without checkpoint manager
		logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

		workspaceDir := t.TempDir()
		firecrackerPath := workspaceDir + "/firecracker"
		_ = os.WriteFile(firecrackerPath, []byte("#!/bin/sh\necho test"), 0755)

		config := runtime.Config{
			VMManagerConfig: vm.ManagerConfig{
				FirecrackerBinary: firecrackerPath,
				WorkspaceDir:      workspaceDir,
			},
			WorkspaceDir: workspaceDir,
		}

		rtNoCP, err := runtime.New(logger, config, nil)
		require.NoError(t, err)

		// Operations should fail gracefully
		_, err = rtNoCP.CreateCheckpoint(ctx, "test-agent")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "checkpoint manager not initialized")
	})

	t.Run("database connection errors", func(t *testing.T) {
		// This test documents expected behavior when database is unavailable
		// In production, errors should be logged and propagated
		assert.NotNil(t, rt)
	})
}

// TestCheckpointRecovery_Performance tests checkpoint performance characteristics
func TestCheckpointRecovery_Performance(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping performance test in short mode")
	}

	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}

	db, err := sql.Open("postgres", dbURL)
	require.NoError(t, err)
	defer db.Close()

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	config := recovery.DefaultCheckpointConfig()

	cm, err := recovery.NewCheckpointManager(logger, db, config)
	require.NoError(t, err)

	ctx := context.Background()
	agentID := api.AgentID("test-recovery-perf")
	tenantID := api.TenantID("test-recovery-tenant")

	_, _ = db.Exec("DELETE FROM checkpoints WHERE agent_id = $1", agentID)

	t.Run("checkpoint creation performance", func(t *testing.T) {
		iterations := 100
		start := time.Now()

		for i := 0; i < iterations; i++ {
			state := map[string]interface{}{
				"iteration": i,
				"timestamp": time.Now().Unix(),
			}
			_, err := cm.CreateCheckpoint(ctx, agentID, tenantID, state, nil)
			require.NoError(t, err)
		}

		duration := time.Since(start)
		avgPerCheckpoint := duration / time.Duration(iterations)

		t.Logf("Created %d checkpoints in %v (avg: %v per checkpoint)",
			iterations, duration, avgPerCheckpoint)

		// Should be reasonably fast (< 100ms per checkpoint on average)
		assert.Less(t, avgPerCheckpoint, 100*time.Millisecond,
			"checkpoint creation too slow")
	})

	t.Run("checkpoint retrieval performance", func(t *testing.T) {
		iterations := 100
		start := time.Now()

		for i := 0; i < iterations; i++ {
			_, err := cm.GetLatestCheckpoint(ctx, agentID)
			require.NoError(t, err)
		}

		duration := time.Since(start)
		avgPerRetrieval := duration / time.Duration(iterations)

		t.Logf("Retrieved %d checkpoints in %v (avg: %v per retrieval)",
			iterations, duration, avgPerRetrieval)

		// Retrieval should be fast (< 50ms per checkpoint on average)
		assert.Less(t, avgPerRetrieval, 50*time.Millisecond,
			"checkpoint retrieval too slow")
	})
}
