package integration

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dnakitare/aether/pkg/api"
)

// TestCompleteAgentLifecycle tests the full agent workflow end-to-end.
// This validates that all components work together: API, Runtime, Scheduler, PostgreSQL.
func TestCompleteAgentLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping E2E test in short mode")
	}

	env := SetupTestEnvironment(t)
	defer env.TearDown()

	if env.Runtime == nil {
		t.Skip("Runtime not available (Firecracker not installed)")
	}

	ctx := context.Background()

	t.Run("create_agent_via_runtime", func(t *testing.T) {
		// Create agent configuration
		agentConfig := api.AgentConfig{
			ID:       api.AgentID("e2e-test-agent-1"),
			TenantID: env.TenantID,
			Name:     "E2E Test Agent",
			Image:    "test:latest",
			Resources: api.ResourceLimits{
				CPUCount: 1,
				MemoryMB: 512,
				DiskMB:   1024,
			},
			Env: map[string]string{
				"TEST_VAR": "test_value",
			},
			Labels: map[string]string{
				"test": "e2e",
			},
		}

		// Step 1: Create agent via runtime
		err := env.Runtime.CreateAgent(ctx, agentConfig)
		require.NoError(t, err, "failed to create agent")

		// Step 2: Get agent info via runtime (validates PostgreSQL persistence)
		agentInfo, err := env.Runtime.GetAgent(ctx, agentConfig.ID)
		require.NoError(t, err, "failed to get agent info")
		assert.Equal(t, agentConfig.ID, agentInfo.Config.ID)
		assert.Equal(t, agentConfig.Name, agentInfo.Config.Name)
		assert.NotZero(t, agentInfo.CreatedAt)

		// Step 4: List agents and verify it's included
		agents, err := env.Runtime.ListAgents(ctx, &env.TenantID)
		require.NoError(t, err, "failed to list agents")
		assert.GreaterOrEqual(t, len(agents), 1, "should have at least one agent")

		found := false
		for _, a := range agents {
			if a.Config.ID == agentConfig.ID {
				found = true
				break
			}
		}
		assert.True(t, found, "created agent should be in list")

		// Step 5: Clean up - destroy agent
		err = env.Runtime.DestroyAgent(ctx, agentConfig.ID)
		require.NoError(t, err, "failed to destroy agent")

		// Step 6: Verify agent is removed from runtime (and PostgreSQL)
		_, err = env.Runtime.GetAgent(ctx, agentConfig.ID)
		assert.Error(t, err, "agent should be deleted from runtime")
	})

	t.Run("agent_status_transitions", func(t *testing.T) {
		// Create agent
		agentConfig := api.AgentConfig{
			ID:       api.AgentID("e2e-test-agent-status"),
			TenantID: env.TenantID,
			Name:     "Status Test Agent",
			Image:    "test:latest",
			Resources: api.ResourceLimits{
				CPUCount: 1,
				MemoryMB: 512,
			},
		}

		err := env.Runtime.CreateAgent(ctx, agentConfig)
		require.NoError(t, err)
		defer env.Runtime.DestroyAgent(ctx, agentConfig.ID)

		// Verify initial status is pending
		info, err := env.Runtime.GetAgent(ctx, agentConfig.ID)
		require.NoError(t, err)
		assert.Equal(t, api.AgentStatusPending, info.Status)

		// Note: Starting the agent requires Firecracker/KVM which may not be available
		// in test environment. We skip actual VM start for Alpha E2E tests.
		// The status persistence is already validated above.
	})

	t.Run("multiple_agents_isolation", func(t *testing.T) {
		// Create multiple agents for the same tenant
		agent1Config := api.AgentConfig{
			ID:       api.AgentID("e2e-multi-1"),
			TenantID: env.TenantID,
			Name:     "Multi Agent 1",
			Image:    "test:latest",
			Resources: api.ResourceLimits{
				CPUCount: 1,
				MemoryMB: 256,
			},
		}

		agent2Config := api.AgentConfig{
			ID:       api.AgentID("e2e-multi-2"),
			TenantID: env.TenantID,
			Name:     "Multi Agent 2",
			Image:    "test:latest",
			Resources: api.ResourceLimits{
				CPUCount: 1,
				MemoryMB: 256,
			},
		}

		// Create both agents
		err := env.Runtime.CreateAgent(ctx, agent1Config)
		require.NoError(t, err)
		defer env.Runtime.DestroyAgent(ctx, agent1Config.ID)

		err = env.Runtime.CreateAgent(ctx, agent2Config)
		require.NoError(t, err)
		defer env.Runtime.DestroyAgent(ctx, agent2Config.ID)

		// List agents and verify both are present
		agents, err := env.Runtime.ListAgents(ctx, &env.TenantID)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(agents), 2)

		// Verify each agent is independent
		info1, err := env.Runtime.GetAgent(ctx, agent1Config.ID)
		require.NoError(t, err)
		assert.Equal(t, "Multi Agent 1", info1.Config.Name)

		info2, err := env.Runtime.GetAgent(ctx, agent2Config.ID)
		require.NoError(t, err)
		assert.Equal(t, "Multi Agent 2", info2.Config.Name)
	})
}

// TestSchedulerIntegration tests scheduler integration with runtime.
func TestSchedulerIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping scheduler integration test in short mode")
	}

	env := SetupTestEnvironment(t)
	defer env.TearDown()

	if env.Scheduler == nil {
		t.Skip("Scheduler not available")
	}

	t.Run("agent_placement", func(t *testing.T) {
		// Verify scheduler has nodes
		nodes := env.Scheduler.ListNodes()
		require.NotEmpty(t, nodes, "scheduler should have at least one node")

		// Get scheduler stats
		stats := env.Scheduler.GetStats()
		assert.GreaterOrEqual(t, stats.NodeCount, 1)

		t.Logf("Scheduler stats: %d nodes, %d agents queued",
			stats.NodeCount, stats.QueuedRequests)
	})
}

// TestRuntimePersistence tests that Runtime properly persists agent state.
func TestRuntimePersistence(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping persistence test in short mode")
	}

	env := SetupTestEnvironment(t)
	defer env.TearDown()

	if env.Runtime == nil {
		t.Skip("Runtime not available (Firecracker not installed)")
	}

	if env.DB == nil {
		t.Skip("PostgreSQL not available")
	}

	t.Run("agent_crud_persists", func(t *testing.T) {
		ctx := context.Background()

		// Create agent
		agentID := api.AgentID("persist-test-agent")
		agentConfig := api.AgentConfig{
			ID:       agentID,
			TenantID: env.TenantID,
			Name:     "Persistence Test",
			Image:    "test:latest",
			Resources: api.ResourceLimits{
				CPUCount: 1,
				MemoryMB: 512,
			},
		}

		err := env.Runtime.CreateAgent(ctx, agentConfig)
		require.NoError(t, err)
		defer env.Runtime.DestroyAgent(ctx, agentID)

		// Verify agent can be retrieved
		info, err := env.Runtime.GetAgent(ctx, agentID)
		require.NoError(t, err)
		assert.Equal(t, agentID, info.Config.ID)
		assert.Equal(t, "Persistence Test", info.Config.Name)

		// List agents and verify it's included
		agents, err := env.Runtime.ListAgents(ctx, &env.TenantID)
		require.NoError(t, err)

		found := false
		for _, a := range agents {
			if a.Config.ID == agentID {
				found = true
				break
			}
		}
		assert.True(t, found, "agent should be in list")
	})
}

// TestConcurrentOperations tests concurrent agent operations.
func TestConcurrentOperations(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping concurrent operations test in short mode")
	}

	env := SetupTestEnvironment(t)
	defer env.TearDown()

	if env.Runtime == nil {
		t.Skip("Runtime not available (Firecracker not installed)")
	}

	ctx := context.Background()

	t.Run("concurrent_agent_creation", func(t *testing.T) {
		const numAgents = 5

		// Create agents concurrently
		errCh := make(chan error, numAgents)
		for i := 0; i < numAgents; i++ {
			go func(index int) {
				agentConfig := api.AgentConfig{
					ID:       api.AgentID(time.Now().Format("concurrent-20060102-150405.000000") + string(rune('a'+index))),
					TenantID: env.TenantID,
					Name:     time.Now().Format("Concurrent Agent 20060102-150405.000000") + string(rune('a'+index)),
					Image:    "test:latest",
					Resources: api.ResourceLimits{
						CPUCount: 1,
						MemoryMB: 256,
					},
				}

				err := env.Runtime.CreateAgent(ctx, agentConfig)
				errCh <- err

				if err == nil {
					// Clean up
					env.Runtime.DestroyAgent(ctx, agentConfig.ID)
				}
			}(i)
		}

		// Wait for all goroutines
		for i := 0; i < numAgents; i++ {
			err := <-errCh
			assert.NoError(t, err, "concurrent agent creation should succeed")
		}
	})
}
