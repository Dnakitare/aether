// Package integration contains data integrity integration tests.
// These tests require a PostgreSQL database.
//
// Run with: TEST_DATABASE_URL="postgres://..." go test -v ./tests/integration/
package integration

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	_ "github.com/lib/pq"

	"github.com/aether-runtime/aether/internal/recovery"
	"github.com/aether-runtime/aether/pkg/api"
)

// setupTestDB creates a test database connection.
func setupTestDB(t *testing.T) *sql.DB {
	// Use test database from environment or skip
	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("TEST_DATABASE_URL not set, skipping integration tests")
	}

	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		t.Fatalf("failed to connect to test database: %v", err)
	}

	// Clean up test data
	_, err = db.Exec("DELETE FROM checkpoints")
	if err != nil {
		t.Fatalf("failed to clean test data: %v", err)
	}

	// Create test tenant if it doesn't exist (for foreign key constraint)
	_, err = db.Exec(`
		INSERT INTO tenants (id, name, tier, created_at, updated_at)
		VALUES ('test-tenant', 'Test Tenant', 'free', NOW(), NOW())
		ON CONFLICT (id) DO NOTHING
	`)
	if err != nil {
		t.Fatalf("failed to create test tenant: %v", err)
	}

	return db
}

// TestCheckpointTransactionAtomicity verifies that checkpoint creation and cleanup are atomic.
func TestCheckpointTransactionAtomicity(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	config := recovery.DefaultCheckpointConfig()
	config.RetentionCount = 3

	cm, err := recovery.NewCheckpointManager(logger, db, config)
	if err != nil {
		t.Fatalf("failed to create checkpoint manager: %v", err)
	}

	ctx := context.Background()
	agentID := api.AgentID("test-agent-atomic")
	tenantID := api.TenantID("test-tenant")

	// Create 5 checkpoints
	for i := 1; i <= 5; i++ {
		state := map[string]interface{}{
			"step": i,
			"data": fmt.Sprintf("checkpoint-%d", i),
		}
		metadata := map[string]string{
			"version": fmt.Sprintf("v%d", i),
		}

		cp, err := cm.CreateCheckpoint(ctx, agentID, tenantID, state, metadata)
		if err != nil {
			t.Fatalf("failed to create checkpoint %d: %v", i, err)
		}

		if cp.Version != i {
			t.Errorf("expected version %d, got %d", i, cp.Version)
		}
	}

	// Verify only 3 checkpoints remain (retention policy enforced atomically)
	checkpoints, err := cm.ListCheckpoints(ctx, agentID)
	if err != nil {
		t.Fatalf("failed to list checkpoints: %v", err)
	}

	if len(checkpoints) != 3 {
		t.Errorf("expected 3 checkpoints after retention cleanup, got %d", len(checkpoints))
	}

	// Verify versions are 5, 4, 3 (descending order)
	expectedVersions := []int{5, 4, 3}
	for i, cp := range checkpoints {
		if cp.Version != expectedVersions[i] {
			t.Errorf("checkpoint %d: expected version %d, got %d", i, expectedVersions[i], cp.Version)
		}
	}
}

// TestCheckpointConcurrentCreation verifies no duplicate versions under concurrent load.
// This is the critical test for transaction isolation.
func TestCheckpointConcurrentCreation(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	config := recovery.DefaultCheckpointConfig()
	config.RetentionCount = 100 // Keep all checkpoints for this test

	cm, err := recovery.NewCheckpointManager(logger, db, config)
	if err != nil {
		t.Fatalf("failed to create checkpoint manager: %v", err)
	}

	ctx := context.Background()
	agentID := api.AgentID("test-agent-concurrent")
	tenantID := api.TenantID("test-tenant")

	// Create 50 checkpoints concurrently from 10 goroutines
	concurrency := 10
	checkpointsPerGoroutine := 5
	totalCheckpoints := concurrency * checkpointsPerGoroutine

	var wg sync.WaitGroup
	errors := make(chan error, concurrency)

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()
			for j := 0; j < checkpointsPerGoroutine; j++ {
				state := map[string]interface{}{
					"goroutine": goroutineID,
					"iteration": j,
					"timestamp": time.Now().UnixNano(),
				}
				metadata := map[string]string{
					"source": fmt.Sprintf("goroutine-%d", goroutineID),
				}

				_, err := cm.CreateCheckpoint(ctx, agentID, tenantID, state, metadata)
				if err != nil {
					errors <- fmt.Errorf("goroutine %d iteration %d: %w", goroutineID, j, err)
					return
				}
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	// Check for errors
	for err := range errors {
		t.Errorf("concurrent checkpoint creation error: %v", err)
	}

	// Verify we have exactly the expected number of checkpoints
	checkpoints, err := cm.ListCheckpoints(ctx, agentID)
	if err != nil {
		t.Fatalf("failed to list checkpoints: %v", err)
	}

	if len(checkpoints) != totalCheckpoints {
		t.Errorf("expected %d checkpoints, got %d", totalCheckpoints, len(checkpoints))
	}

	// Verify no duplicate versions (critical for atomicity)
	versionMap := make(map[int]bool)
	for _, cp := range checkpoints {
		if versionMap[cp.Version] {
			t.Errorf("RACE CONDITION DETECTED: duplicate version found: %d", cp.Version)
		}
		versionMap[cp.Version] = true
	}

	// Verify versions are sequential from 1 to totalCheckpoints
	for i := 1; i <= totalCheckpoints; i++ {
		if !versionMap[i] {
			t.Errorf("missing version: %d", i)
		}
	}
}

// TestCheckpointTransactionRollback verifies rollback on error.
func TestCheckpointTransactionRollback(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	config := recovery.DefaultCheckpointConfig()
	config.MaxCheckpointSize = 100 // Very small size to trigger error

	cm, err := recovery.NewCheckpointManager(logger, db, config)
	if err != nil {
		t.Fatalf("failed to create checkpoint manager: %v", err)
	}

	ctx := context.Background()
	agentID := api.AgentID("test-agent-rollback")
	tenantID := api.TenantID("test-tenant")

	// Create a valid checkpoint first
	smallState := map[string]interface{}{"valid": true}
	cp1, err := cm.CreateCheckpoint(ctx, agentID, tenantID, smallState, nil)
	if err != nil {
		t.Fatalf("failed to create valid checkpoint: %v", err)
	}

	// Try to create a checkpoint that's too large
	largeState := map[string]interface{}{
		"data": string(make([]byte, 1000)), // 1KB, larger than max
	}

	_, err = cm.CreateCheckpoint(ctx, agentID, tenantID, largeState, nil)
	if err == nil {
		t.Fatal("expected error for oversized checkpoint, got nil")
	}

	// Verify transaction was rolled back (only 1 checkpoint exists)
	checkpoints, err := cm.ListCheckpoints(ctx, agentID)
	if err != nil {
		t.Fatalf("failed to list checkpoints: %v", err)
	}

	if len(checkpoints) != 1 {
		t.Errorf("expected 1 checkpoint after rollback, got %d", len(checkpoints))
	}

	// Verify version is still 1 (no version increment occurred)
	if checkpoints[0].Version != cp1.Version {
		t.Errorf("version changed despite rollback: expected %d, got %d", cp1.Version, checkpoints[0].Version)
	}
}

// TestCheckpointRetentionCleanup verifies old checkpoints are cleaned up atomically.
func TestCheckpointRetentionCleanup(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	config := recovery.DefaultCheckpointConfig()
	config.RetentionCount = 5

	cm, err := recovery.NewCheckpointManager(logger, db, config)
	if err != nil {
		t.Fatalf("failed to create checkpoint manager: %v", err)
	}

	ctx := context.Background()
	agentID := api.AgentID("test-agent-retention")
	tenantID := api.TenantID("test-tenant")

	// Create 10 checkpoints
	for i := 1; i <= 10; i++ {
		state := map[string]interface{}{
			"step":      i,
			"timestamp": time.Now().UnixNano(),
		}
		_, err := cm.CreateCheckpoint(ctx, agentID, tenantID, state, nil)
		if err != nil {
			t.Fatalf("failed to create checkpoint %d: %v", i, err)
		}
	}

	// Verify only 5 most recent checkpoints exist
	checkpoints, err := cm.ListCheckpoints(ctx, agentID)
	if err != nil {
		t.Fatalf("failed to list checkpoints: %v", err)
	}

	if len(checkpoints) != 5 {
		t.Errorf("expected 5 checkpoints after retention cleanup, got %d", len(checkpoints))
	}

	// Verify versions are 10, 9, 8, 7, 6 (descending)
	for i, cp := range checkpoints {
		expectedVersion := 10 - i
		if cp.Version != expectedVersion {
			t.Errorf("checkpoint %d: expected version %d, got %d", i, expectedVersion, cp.Version)
		}
	}

	// Verify old checkpoints (versions 1-5) were deleted
	for v := 1; v <= 5; v++ {
		_, err := cm.GetCheckpointByVersion(ctx, agentID, v)
		if err == nil {
			t.Errorf("old checkpoint version %d should have been deleted", v)
		}
	}
}

// TestCheckpointConcurrentReadWrite verifies reads don't block writes and vice versa.
func TestCheckpointConcurrentReadWrite(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	config := recovery.DefaultCheckpointConfig()
	config.RetentionCount = 100

	cm, err := recovery.NewCheckpointManager(logger, db, config)
	if err != nil {
		t.Fatalf("failed to create checkpoint manager: %v", err)
	}

	ctx := context.Background()
	agentID := api.AgentID("test-agent-readwrite")
	tenantID := api.TenantID("test-tenant")

	// Create initial checkpoint
	state := map[string]interface{}{"initial": true}
	_, err = cm.CreateCheckpoint(ctx, agentID, tenantID, state, nil)
	if err != nil {
		t.Fatalf("failed to create initial checkpoint: %v", err)
	}

	var wg sync.WaitGroup
	errors := make(chan error, 100)
	startSignal := make(chan struct{})

	// Start writers
	writerCount := 10
	writesPerWriter := 5
	for i := 0; i < writerCount; i++ {
		wg.Add(1)
		go func(writerID int) {
			defer wg.Done()
			<-startSignal // Wait for start signal
			for j := 0; j < writesPerWriter; j++ {
				state := map[string]interface{}{
					"writer": writerID,
					"iter":   j,
					"time":   time.Now().UnixNano(),
				}
				_, err := cm.CreateCheckpoint(ctx, agentID, tenantID, state, nil)
				if err != nil {
					errors <- fmt.Errorf("writer %d: %w", writerID, err)
					return
				}
				time.Sleep(5 * time.Millisecond)
			}
		}(i)
	}

	// Start readers
	readerCount := 10
	readsPerReader := 10
	for i := 0; i < readerCount; i++ {
		wg.Add(1)
		go func(readerID int) {
			defer wg.Done()
			<-startSignal // Wait for start signal
			for j := 0; j < readsPerReader; j++ {
				_, err := cm.GetLatestCheckpoint(ctx, agentID)
				if err != nil {
					errors <- fmt.Errorf("reader %d: %w", readerID, err)
					return
				}
				time.Sleep(2 * time.Millisecond)
			}
		}(i)
	}

	// Start all goroutines simultaneously
	close(startSignal)

	wg.Wait()
	close(errors)

	// Check for errors
	for err := range errors {
		t.Errorf("concurrent read/write error: %v", err)
	}

	// Verify expected number of checkpoints created
	checkpoints, err := cm.ListCheckpoints(ctx, agentID)
	if err != nil {
		t.Fatalf("failed to list checkpoints: %v", err)
	}

	expectedCount := 1 + (writerCount * writesPerWriter) // initial + written
	if len(checkpoints) != expectedCount {
		t.Errorf("expected %d checkpoints, got %d", expectedCount, len(checkpoints))
	}
}

// TestCheckpointMultipleAgents verifies isolation between different agents.
func TestCheckpointMultipleAgents(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	config := recovery.DefaultCheckpointConfig()
	config.RetentionCount = 100

	cm, err := recovery.NewCheckpointManager(logger, db, config)
	if err != nil {
		t.Fatalf("failed to create checkpoint manager: %v", err)
	}

	ctx := context.Background()
	tenantID := api.TenantID("test-tenant")

	// Create checkpoints for 3 different agents concurrently
	agentCount := 3
	checkpointsPerAgent := 10

	var wg sync.WaitGroup
	errors := make(chan error, agentCount)

	for a := 0; a < agentCount; a++ {
		wg.Add(1)
		go func(agentNum int) {
			defer wg.Done()
			agentID := api.AgentID(fmt.Sprintf("test-agent-%d", agentNum))

			for i := 1; i <= checkpointsPerAgent; i++ {
				state := map[string]interface{}{
					"agent": agentNum,
					"step":  i,
				}
				cp, err := cm.CreateCheckpoint(ctx, agentID, tenantID, state, nil)
				if err != nil {
					errors <- fmt.Errorf("agent %d: %w", agentNum, err)
					return
				}

				// Verify each agent has sequential versions starting from 1
				if cp.Version != i {
					errors <- fmt.Errorf("agent %d: expected version %d, got %d", agentNum, i, cp.Version)
					return
				}
			}
		}(a)
	}

	wg.Wait()
	close(errors)

	// Check for errors
	for err := range errors {
		t.Errorf("multi-agent test error: %v", err)
	}

	// Verify each agent has correct number of checkpoints
	for a := 0; a < agentCount; a++ {
		agentID := api.AgentID(fmt.Sprintf("test-agent-%d", a))
		checkpoints, err := cm.ListCheckpoints(ctx, agentID)
		if err != nil {
			t.Errorf("failed to list checkpoints for agent %d: %v", a, err)
			continue
		}

		if len(checkpoints) != checkpointsPerAgent {
			t.Errorf("agent %d: expected %d checkpoints, got %d", a, checkpointsPerAgent, len(checkpoints))
		}

		// Verify versions are 1 to checkpointsPerAgent
		for i, cp := range checkpoints {
			expectedVersion := checkpointsPerAgent - i
			if cp.Version != expectedVersion {
				t.Errorf("agent %d checkpoint %d: expected version %d, got %d", a, i, expectedVersion, cp.Version)
			}
		}
	}
}
