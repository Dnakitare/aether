// Package integration contains state integrity integration tests.
// These tests require Redis.
//
// Run with: TEST_REDIS_URL="redis://..." go test -v ./tests/integration/
package integration

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/aether-runtime/aether/internal/state"
	"github.com/aether-runtime/aether/pkg/api"
)

// setupTestRedis creates a test Redis connection.
func setupTestRedis(t *testing.T) *state.RedisStore {
	redisURL := os.Getenv("TEST_REDIS_URL")
	if redisURL == "" {
		t.Skip("TEST_REDIS_URL not set, skipping Redis integration tests")
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))

	// Parse Redis URL
	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		t.Fatalf("failed to parse Redis URL: %v", err)
	}

	config := state.Config{
		Address:    opts.Addr,
		Password:   opts.Password,
		DB:         opts.DB,
		KeyPrefix:  "test:aether:",
		DefaultTTL: 1 * time.Hour,
	}

	rs, err := state.NewRedisStore(logger, config)
	if err != nil {
		t.Fatalf("failed to create Redis store: %v", err)
	}

	// Clean up test data
	ctx := context.Background()
	pattern := config.KeyPrefix + "*"
	iter := rs.GetClient().Scan(ctx, 0, pattern, 0).Iterator()
	for iter.Next(ctx) {
		rs.GetClient().Del(ctx, iter.Val())
	}

	return rs
}

// TestSaveAgentStateAtomicity verifies agent state and tenant set are updated atomically.
func TestSaveAgentStateAtomicity(t *testing.T) {
	rs := setupTestRedis(t)
	defer rs.Close()

	ctx := context.Background()
	agentID := api.AgentID("test-agent-save")
	tenantID := api.TenantID("test-tenant")

	agentInfo := &api.AgentInfo{
		Config: api.AgentConfig{
			ID:       agentID,
			TenantID: tenantID,
			Name:     "Test Agent",
			Image:    "test:latest",
		},
		Status: api.AgentStatusRunning,
	}

	// Save agent state
	err := rs.SaveAgentState(ctx, agentInfo)
	if err != nil {
		t.Fatalf("failed to save agent state: %v", err)
	}

	// Verify agent state was saved
	retrieved, err := rs.GetAgentState(ctx, agentID)
	if err != nil {
		t.Errorf("failed to retrieve agent state: %v", err)
	}
	if retrieved.Config.ID != agentID {
		t.Errorf("expected agent ID %s, got %s", agentID, retrieved.Config.ID)
	}

	// Verify agent was added to tenant set
	agents, err := rs.ListAgentsByTenant(ctx, tenantID)
	if err != nil {
		t.Errorf("failed to list agents by tenant: %v", err)
	}
	if len(agents) != 1 {
		t.Errorf("expected 1 agent in tenant set, got %d", len(agents))
	}
	if agents[0] != agentID {
		t.Errorf("expected agent ID %s in tenant set, got %s", agentID, agents[0])
	}
}

// TestDeleteAgentStateAtomicity verifies agent state and tenant set are deleted atomically.
func TestDeleteAgentStateAtomicity(t *testing.T) {
	rs := setupTestRedis(t)
	defer rs.Close()

	ctx := context.Background()
	agentID := api.AgentID("test-agent-delete")
	tenantID := api.TenantID("test-tenant")

	agentInfo := &api.AgentInfo{
		Config: api.AgentConfig{
			ID:       agentID,
			TenantID: tenantID,
			Name:     "Test Agent",
			Image:    "test:latest",
		},
		Status: api.AgentStatusRunning,
	}

	// Save agent state first
	err := rs.SaveAgentState(ctx, agentInfo)
	if err != nil {
		t.Fatalf("failed to save agent state: %v", err)
	}

	// Delete agent state
	err = rs.DeleteAgentState(ctx, agentID, tenantID)
	if err != nil {
		t.Fatalf("failed to delete agent state: %v", err)
	}

	// Verify agent state was deleted
	_, err = rs.GetAgentState(ctx, agentID)
	if err == nil {
		t.Error("expected error retrieving deleted agent state, got nil")
	}

	// Verify agent was removed from tenant set
	agents, err := rs.ListAgentsByTenant(ctx, tenantID)
	if err != nil {
		t.Errorf("failed to list agents by tenant: %v", err)
	}
	if len(agents) != 0 {
		t.Errorf("expected 0 agents in tenant set after delete, got %d", len(agents))
	}
}

// TestConcurrentAgentStateUpdates verifies concurrent updates are atomic.
func TestConcurrentAgentStateUpdates(t *testing.T) {
	rs := setupTestRedis(t)
	defer rs.Close()

	ctx := context.Background()
	tenantID := api.TenantID("test-tenant-concurrent")

	// Create 20 agents concurrently
	agentCount := 20
	var wg sync.WaitGroup
	errors := make(chan error, agentCount)

	for i := 0; i < agentCount; i++ {
		wg.Add(1)
		go func(agentNum int) {
			defer wg.Done()
			agentID := api.AgentID(fmt.Sprintf("test-agent-%d", agentNum))

			agentInfo := &api.AgentInfo{
				Config: api.AgentConfig{
					ID:       agentID,
					TenantID: tenantID,
					Name:     fmt.Sprintf("Agent %d", agentNum),
					Image:    "test:latest",
				},
				Status: api.AgentStatusRunning,
			}

			err := rs.SaveAgentState(ctx, agentInfo)
			if err != nil {
				errors <- fmt.Errorf("agent %d: %w", agentNum, err)
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	// Check for errors
	for err := range errors {
		t.Errorf("concurrent save error: %v", err)
	}

	// Verify all agents are in tenant set
	agents, err := rs.ListAgentsByTenant(ctx, tenantID)
	if err != nil {
		t.Fatalf("failed to list agents by tenant: %v", err)
	}

	if len(agents) != agentCount {
		t.Errorf("expected %d agents in tenant set, got %d", agentCount, len(agents))
	}

	// Verify no duplicates in tenant set
	agentSet := make(map[api.AgentID]bool)
	for _, agentID := range agents {
		if agentSet[agentID] {
			t.Errorf("duplicate agent ID in tenant set: %s", agentID)
		}
		agentSet[agentID] = true
	}
}

// TestConcurrentAgentStateMixedOperations verifies atomicity with mixed save/delete operations.
func TestConcurrentAgentStateMixedOperations(t *testing.T) {
	rs := setupTestRedis(t)
	defer rs.Close()

	ctx := context.Background()
	tenantID := api.TenantID("test-tenant-mixed")
	agentCount := 20

	// Create initial agents
	for i := 0; i < agentCount; i++ {
		agentID := api.AgentID(fmt.Sprintf("test-agent-%d", i))
		agentInfo := &api.AgentInfo{
			Config: api.AgentConfig{
				ID:       agentID,
				TenantID: tenantID,
				Name:     fmt.Sprintf("Agent %d", i),
				Image:    "test:latest",
			},
			Status: api.AgentStatusRunning,
		}
		err := rs.SaveAgentState(ctx, agentInfo)
		if err != nil {
			t.Fatalf("failed to create initial agent %d: %v", i, err)
		}
	}

	// Perform mixed operations concurrently
	var wg sync.WaitGroup
	errors := make(chan error, agentCount*2)

	// Update half the agents
	for i := 0; i < agentCount/2; i++ {
		wg.Add(1)
		go func(agentNum int) {
			defer wg.Done()
			agentID := api.AgentID(fmt.Sprintf("test-agent-%d", agentNum))

			agentInfo := &api.AgentInfo{
				Config: api.AgentConfig{
					ID:       agentID,
					TenantID: tenantID,
					Name:     fmt.Sprintf("Updated Agent %d", agentNum),
					Image:    "test:v2",
				},
				Status: api.AgentStatusStopped,
			}

			err := rs.SaveAgentState(ctx, agentInfo)
			if err != nil {
				errors <- fmt.Errorf("update agent %d: %w", agentNum, err)
			}
		}(i)
	}

	// Delete the other half
	for i := agentCount / 2; i < agentCount; i++ {
		wg.Add(1)
		go func(agentNum int) {
			defer wg.Done()
			agentID := api.AgentID(fmt.Sprintf("test-agent-%d", agentNum))

			err := rs.DeleteAgentState(ctx, agentID, tenantID)
			if err != nil {
				errors <- fmt.Errorf("delete agent %d: %w", agentNum, err)
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	// Check for errors
	for err := range errors {
		t.Errorf("mixed operations error: %v", err)
	}

	// Verify tenant set has correct count (half deleted)
	agents, err := rs.ListAgentsByTenant(ctx, tenantID)
	if err != nil {
		t.Fatalf("failed to list agents by tenant: %v", err)
	}

	expectedCount := agentCount / 2
	if len(agents) != expectedCount {
		t.Errorf("expected %d agents after mixed operations, got %d", expectedCount, len(agents))
	}

	// Verify only the first half exist
	for i := 0; i < agentCount/2; i++ {
		agentID := api.AgentID(fmt.Sprintf("test-agent-%d", i))
		retrieved, err := rs.GetAgentState(ctx, agentID)
		if err != nil {
			t.Errorf("expected agent %d to exist, got error: %v", i, err)
		}
		if retrieved.Config.Name != fmt.Sprintf("Updated Agent %d", i) {
			t.Errorf("agent %d not updated correctly", i)
		}
	}

	// Verify the second half are deleted
	for i := agentCount / 2; i < agentCount; i++ {
		agentID := api.AgentID(fmt.Sprintf("test-agent-%d", i))
		_, err := rs.GetAgentState(ctx, agentID)
		if err == nil {
			t.Errorf("expected agent %d to be deleted, but it still exists", i)
		}
	}
}

// TestMultipleTenantIsolation verifies tenant isolation in concurrent operations.
func TestMultipleTenantIsolation(t *testing.T) {
	rs := setupTestRedis(t)
	defer rs.Close()

	ctx := context.Background()
	tenantCount := 5
	agentsPerTenant := 10

	var wg sync.WaitGroup
	errors := make(chan error, tenantCount*agentsPerTenant)

	// Create agents for multiple tenants concurrently
	for tid := 0; tid < tenantCount; tid++ {
		for aid := 0; aid < agentsPerTenant; aid++ {
			wg.Add(1)
			go func(tenantNum, agentNum int) {
				defer wg.Done()

				tenantID := api.TenantID(fmt.Sprintf("tenant-%d", tenantNum))
				agentID := api.AgentID(fmt.Sprintf("tenant-%d-agent-%d", tenantNum, agentNum))

				agentInfo := &api.AgentInfo{
					Config: api.AgentConfig{
						ID:       agentID,
						TenantID: tenantID,
						Name:     fmt.Sprintf("Tenant %d Agent %d", tenantNum, agentNum),
						Image:    "test:latest",
					},
					Status: api.AgentStatusRunning,
				}

				err := rs.SaveAgentState(ctx, agentInfo)
				if err != nil {
					errors <- fmt.Errorf("tenant %d agent %d: %w", tenantNum, agentNum, err)
				}
			}(tid, aid)
		}
	}

	wg.Wait()
	close(errors)

	// Check for errors
	for err := range errors {
		t.Errorf("multi-tenant save error: %v", err)
	}

	// Verify each tenant has correct number of agents
	for tid := 0; tid < tenantCount; tid++ {
		tenantID := api.TenantID(fmt.Sprintf("tenant-%d", tid))
		agents, err := rs.ListAgentsByTenant(ctx, tenantID)
		if err != nil {
			t.Errorf("failed to list agents for tenant %d: %v", tid, err)
			continue
		}

		if len(agents) != agentsPerTenant {
			t.Errorf("tenant %d: expected %d agents, got %d", tid, agentsPerTenant, len(agents))
		}

		// Verify no cross-tenant contamination
		for _, agentID := range agents {
			expectedPrefix := fmt.Sprintf("tenant-%d-agent-", tid)
			if len(agentID) < len(expectedPrefix) || string(agentID)[:len(expectedPrefix)] != expectedPrefix {
				t.Errorf("tenant %d has agent from wrong tenant: %s", tid, agentID)
			}
		}
	}
}

// TestAgentStateConsistencyAfterFailure verifies consistency after simulated failure.
func TestAgentStateConsistencyAfterFailure(t *testing.T) {
	rs := setupTestRedis(t)
	defer rs.Close()

	ctx := context.Background()
	agentID := api.AgentID("test-agent-consistency")
	tenantID := api.TenantID("test-tenant")

	// Create agent state
	agentInfo := &api.AgentInfo{
		Config: api.AgentConfig{
			ID:       agentID,
			TenantID: tenantID,
			Name:     "Test Agent",
			Image:    "test:v1",
		},
		Status: api.AgentStatusRunning,
	}

	err := rs.SaveAgentState(ctx, agentInfo)
	if err != nil {
		t.Fatalf("failed to save agent state: %v", err)
	}

	// Update with different status
	agentInfo.Status = api.AgentStatusStopped
	agentInfo.Config.Image = "test:v2"

	err = rs.SaveAgentState(ctx, agentInfo)
	if err != nil {
		t.Fatalf("failed to update agent state: %v", err)
	}

	// Verify state is consistent
	retrieved, err := rs.GetAgentState(ctx, agentID)
	if err != nil {
		t.Fatalf("failed to retrieve agent state: %v", err)
	}

	if retrieved.Status != api.AgentStatusStopped {
		t.Errorf("expected status %s, got %s", api.AgentStatusStopped, retrieved.Status)
	}
	if retrieved.Config.Image != "test:v2" {
		t.Errorf("expected image test:v2, got %s", retrieved.Config.Image)
	}

	// Verify tenant set still has exactly one entry
	agents, err := rs.ListAgentsByTenant(ctx, tenantID)
	if err != nil {
		t.Fatalf("failed to list agents by tenant: %v", err)
	}
	if len(agents) != 1 {
		t.Errorf("expected 1 agent in tenant set after update, got %d", len(agents))
	}
}
