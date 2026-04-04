package chaos

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dnakitare/aether/internal/scheduler"
	pkgapi "github.com/dnakitare/aether/pkg/api"
)

// TestChaos_StressWithFailures tests system under load with failures
func TestChaos_StressWithFailures(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping chaos stress test in short mode")
	}

	env := SetupChaosEnvironment(t)
	defer env.TearDown()

	ctx := context.Background()

	t.Run("high load scheduling with Redis failures", func(t *testing.T) {
		env.SkipIfNoInfrastructure(t, "redis")

		const numAgents = 50
		const failurePoint = 25

		var wg sync.WaitGroup
		successCount := 0
		failureCount := 0
		var mu sync.Mutex

		// Schedule agents concurrently
		for i := 0; i < numAgents; i++ {
			wg.Add(1)

			go func(idx int) {
				defer wg.Done()

				// Introduce failure midway
				if idx == failurePoint {
					env.SimulateRedisFailure()
					env.T.Log("Redis failure injected at agent", idx)
					time.Sleep(2 * time.Second)
					env.RestoreRedis()
					env.T.Log("Redis restored")
				}

				agentConfig := env.CreateTestAgent("stress-agent")
				resources := scheduler.FromAgentConfig(*agentConfig)
				req := &scheduler.AgentRequest{
					Config:    *agentConfig,
					Resources: resources,
					Priority:  0,
					CreatedAt: time.Now(),
				}

				err := env.Scheduler.ScheduleAgent(ctx, req)

				mu.Lock()
				if err == nil {
					successCount++
				} else {
					failureCount++
				}
				mu.Unlock()
			}(i)

			// Small delay between starts
			time.Sleep(50 * time.Millisecond)
		}

		wg.Wait()

		// Most operations should succeed despite failure
		successRate := float64(successCount) / float64(numAgents)
		assert.Greater(t, successRate, 0.8, "At least 80%% of operations should succeed")

		env.T.Logf("Stress test: %d successes, %d failures (%.1f%% success rate)",
			successCount, failureCount, successRate*100)
	})

	t.Run("rapid failure and recovery cycles", func(t *testing.T) {
		env.SkipIfNoInfrastructure(t, "redis")

		const numCycles = 10
		const opsPerCycle = 5

		totalSuccess := 0
		totalFailure := 0

		for cycle := 0; cycle < numCycles; cycle++ {
			// Perform operations
			for op := 0; op < opsPerCycle; op++ {
				agentInfo := createTestAgentInfo(env)
				err := env.StateStore.SaveAgentState(ctx, agentInfo)
				if err == nil {
					totalSuccess++
				} else {
					totalFailure++
				}
			}

			// Trigger failure
			if cycle%2 == 0 {
				env.SimulateRedisFailure()
				time.Sleep(500 * time.Millisecond)
				env.RestoreRedis()
			}

			time.Sleep(300 * time.Millisecond)
		}

		// System should handle rapid cycles
		env.T.Logf("Rapid cycles: %d successes, %d failures over %d cycles",
			totalSuccess, totalFailure, numCycles)

		assert.Greater(t, totalSuccess, 0, "Some operations should succeed")
	})
}

// TestChaos_MemoryPressure tests behavior under memory pressure with failures
func TestChaos_MemoryPressure(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping chaos memory test in short mode")
	}

	env := SetupChaosEnvironment(t)
	defer env.TearDown()

	t.Run("large agent count with failures", func(t *testing.T) {
		const numAgents = 100

		agentInfos := make([]*pkgapi.AgentInfo, 0, numAgents)

		// Create many agents
		for i := 0; i < numAgents; i++ {
			agentInfo := createTestAgentInfo(env)
			agentInfos = append(agentInfos, agentInfo)

			// Inject failure midway
			if i == numAgents/2 {
				if env.RedisClient != nil {
					env.SimulateRedisFailure()
					time.Sleep(1 * time.Second)
					env.RestoreRedis()
				}
			}
		}

		// Verify scheduler can handle load
		nodes := env.Scheduler.ListNodes()
		assert.NotEmpty(t, nodes)

		stats := env.Scheduler.GetStats()
		assert.NotNil(t, stats)

		env.T.Logf("Memory pressure test completed with %d agents", numAgents)
	})
}

// TestChaos_SlowRecovery tests behavior during slow recovery
func TestChaos_SlowRecovery(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping chaos slow recovery test in short mode")
	}

	env := SetupChaosEnvironment(t)
	defer env.TearDown()

	env.SkipIfNoInfrastructure(t, "redis")

	ctx := context.Background()

	t.Run("operations during slow Redis recovery", func(t *testing.T) {
		// Simulate failure
		err := env.SimulateRedisFailure()
		require.NoError(t, err)

		// Simulate slow recovery (5 seconds)
		go func() {
			time.Sleep(5 * time.Second)
			env.RestoreRedis()
			env.T.Log("Redis recovery completed")
		}()

		// Try operations during slow recovery
		const numAttempts = 20
		eventualSuccess := false

		for i := 0; i < numAttempts; i++ {
			agentInfo := createTestAgentInfo(env)
			err := env.StateStore.SaveAgentState(ctx, agentInfo)
			if err == nil {
				eventualSuccess = true
				env.T.Logf("Operation succeeded on attempt %d", i+1)
				break
			}
			time.Sleep(500 * time.Millisecond)
		}

		assert.True(t, eventualSuccess, "Should eventually succeed after slow recovery")
	})
}

// TestChaos_CascadingFailures tests multiple simultaneous failures
func TestChaos_CascadingFailures(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping chaos cascading test in short mode")
	}

	env := SetupChaosEnvironment(t)
	defer env.TearDown()

	env.SkipIfNoInfrastructure(t, "redis", "postgres")

	ctx := context.Background()

	t.Run("simultaneous service failures", func(t *testing.T) {
		// Create initial state
		agentInfo := createTestAgentInfo(env)
		err := env.StateStore.SaveAgentState(ctx, agentInfo)
		require.NoError(t, err)

		// Trigger cascading failures
		var wg sync.WaitGroup

		wg.Add(1)
		go func() {
			defer wg.Done()
			env.SimulateRedisFailure()
		}()

		wg.Add(1)
		go func() {
			defer wg.Done()
			time.Sleep(500 * time.Millisecond)
			env.SimulatePostgresFailure()
		}()

		wg.Wait()

		env.T.Log("Cascading failures triggered")

		// Verify core functionality still works (in-memory)
		nodes := env.Scheduler.ListNodes()
		assert.NotEmpty(t, nodes, "Scheduler should still function")

		stats := env.Scheduler.GetStats()
		assert.NotNil(t, stats, "Stats should still be accessible")

		// Recovery
		wg.Add(1)
		go func() {
			defer wg.Done()
			env.RestoreRedis()
		}()

		wg.Add(1)
		go func() {
			defer wg.Done()
			time.Sleep(500 * time.Millisecond)
			env.RestorePostgres()
		}()

		wg.Wait()

		// Verify recovery
		time.Sleep(1 * time.Second)

		// State operations should work
		newAgent := createTestAgentInfo(env)
		err = env.StateStore.SaveAgentState(ctx, newAgent)
		assert.NoError(t, err, "State operations should work after recovery")

		env.T.Log("System recovered from cascading failures")
	})
}

// TestChaos_PartialFailures tests partial infrastructure failures
func TestChaos_PartialFailures(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping chaos partial failure test in short mode")
	}

	env := SetupChaosEnvironment(t)
	defer env.TearDown()

	ctx := context.Background()

	t.Run("single node failure in cluster", func(t *testing.T) {
		// Simulate scenario where only one component fails
		// but others remain operational

		// Schedule some agents
		const numAgents = 10

		for i := 0; i < numAgents; i++ {
			agentConfig := env.CreateTestAgent("partial-failure-agent")
			resources := scheduler.FromAgentConfig(*agentConfig)
			req := &scheduler.AgentRequest{
				Config:    *agentConfig,
				Resources: resources,
				Priority:  0,
				CreatedAt: time.Now(),
			}

			err := env.Scheduler.ScheduleAgent(ctx, req)
			require.NoError(t, err)
		}

		time.Sleep(2 * time.Second)

		// Fail only Redis
		if env.RedisClient != nil {
			env.SimulateRedisFailure()
			env.T.Log("Redis failed, other services operational")
		}

		// Scheduler should continue working (in-memory)
		nodes := env.Scheduler.ListNodes()
		assert.NotEmpty(t, nodes)

		// Can still schedule (no Redis dependency for core scheduling)
		agentConfig := env.CreateTestAgent("post-failure-agent")
		resources := scheduler.FromAgentConfig(*agentConfig)
		req := &scheduler.AgentRequest{
			Config:    *agentConfig,
			Resources: resources,
			Priority:  0,
			CreatedAt: time.Now(),
		}

		err := env.Scheduler.ScheduleAgent(ctx, req)
		assert.NoError(t, err, "Scheduling should work without Redis")

		env.T.Log("Partial failure handled gracefully")
	})
}

// TestChaos_LongRunningFailure tests behavior during extended failures
func TestChaos_LongRunningFailure(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping chaos long-running test in short mode")
	}

	env := SetupChaosEnvironment(t)
	defer env.TearDown()

	env.SkipIfNoInfrastructure(t, "redis")

	ctx := context.Background()

	t.Run("extended failure period", func(t *testing.T) {
		// Simulate extended failure (10 seconds)
		err := env.SimulateRedisFailure()
		require.NoError(t, err)

		env.T.Log("Redis down for extended period")

		// Try operations during extended failure
		const duration = 10 * time.Second
		const interval = 1 * time.Second

		start := time.Now()
		operationAttempts := 0
		operationFailures := 0

		for time.Since(start) < duration {
			operationAttempts++

			agentInfo := createTestAgentInfo(env)
			err := env.StateStore.SaveAgentState(ctx, agentInfo)
			if err != nil {
				operationFailures++
			}

			time.Sleep(interval)
		}

		// All operations should fail consistently
		assert.Equal(t, operationAttempts, operationFailures,
			"All operations should fail during extended outage")

		// Restore and verify
		err = env.RestoreRedis()
		require.NoError(t, err)

		// Operations should work immediately after restore
		agentInfo := createTestAgentInfo(env)
		err = env.StateStore.SaveAgentState(ctx, agentInfo)
		assert.NoError(t, err, "Operations should succeed after restore")

		env.T.Logf("Extended failure test: %d operations failed during %v outage",
			operationFailures, duration)
	})
}

/*
Test Coverage Summary:

1. Stress with Failures:
   - High load scheduling with Redis failures (50 agents)
   - Rapid failure and recovery cycles (10 cycles)

2. Memory Pressure:
   - Large agent count with failures (100 agents)

3. Slow Recovery:
   - Operations during slow Redis recovery (5s delay)

4. Cascading Failures:
   - Simultaneous service failures (Redis + PostgreSQL)

5. Partial Failures:
   - Single node failure in cluster

6. Long-Running Failures:
   - Extended failure period (10s outage)

Total Test Cases: 7
Focus: Stress, cascading failures, recovery under load
*/
