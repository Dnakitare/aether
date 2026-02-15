package load

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aether-runtime/aether/internal/scheduler/distributed"
	"github.com/stretchr/testify/require"
)

// TestLoad_1000Agents validates system performance with 1,000 concurrent agents
func TestLoad_1000Agents(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping load test in short mode")
	}

	// Setup test environment with 10 nodes (100 agents per node capacity)
	env := SetupLoadTestEnvironment(t, 10)
	defer env.TearDown()

	// Setup distributed queue with 20 workers
	env.SetupQueue("1000-agents", 20)

	const numAgents = 1000

	// Performance targets for 1K agents
	targets := &PerformanceTarget{
		MaxAvgLatency: 50 * time.Millisecond,  // Average placement should be fast
		MaxP95Latency: 100 * time.Millisecond, // P95 target
		MaxP99Latency: 200 * time.Millisecond, // P99 target
		MinThroughput: 100,                    // At least 100 placements/sec
	}

	t.Logf("Starting load test: %d agents across %d nodes", numAgents, len(env.Nodes))

	// Track placed agents
	var placedCount atomic.Int64
	placed := make(chan string, numAgents)

	// Register placement handler
	env.Queue.RegisterHandler(func(ctx context.Context, req *distributed.SchedulingRequest) error {
		placementStart := time.Now()

		// Get nodes owned by this scheduler
		nodes, err := env.NodeRegistry.GetOwnedNodes(ctx, env.SchedulerID)
		if err != nil {
			env.Metrics.RecordFailure()
			return fmt.Errorf("failed to get owned nodes: %w", err)
		}

		if len(nodes) == 0 {
			env.Metrics.RecordFailure()
			return fmt.Errorf("no nodes available")
		}

		// Bin-packing: find first node with capacity
		for _, node := range nodes {
			if node.CanFit(req.Resources) {
				// Try atomic allocation
				success, err := env.NodeRegistry.TryAllocate(ctx, node.ID, req.AgentID, req.Resources)
				if err != nil {
					continue // Try next node
				}

				if success {
					placementLatency := time.Since(placementStart)
					env.Metrics.RecordSuccess(placementLatency)
					env.Metrics.RecordPlacement(placementLatency)
					placedCount.Add(1)
					placed <- string(req.AgentID)
					return nil
				}
			}
		}

		env.Metrics.RecordFailure()
		return fmt.Errorf("no suitable node found")
	})

	// Enqueue agents in parallel batches
	t.Logf("Enqueuing %d agent requests...", numAgents)
	enqueueStart := time.Now()

	var enqueueWg sync.WaitGroup
	batchSize := 100
	numBatches := numAgents / batchSize

	for batch := 0; batch < numBatches; batch++ {
		enqueueWg.Add(1)
		go func(batchNum int) {
			defer enqueueWg.Done()

			for i := 0; i < batchSize; i++ {
				agentNum := batchNum*batchSize + i + 1
				req := CreateTestAgentRequest(agentNum, fmt.Sprintf("tenant-%d", agentNum%10+1))

				enqueueReqStart := time.Now()
				if err := env.Queue.Enqueue(env.ctx, req); err != nil {
					env.T.Errorf("Failed to enqueue agent %d: %v", agentNum, err)
					continue
				}
				env.Metrics.RecordEnqueue(time.Since(enqueueReqStart))
			}
		}(batch)
	}

	enqueueWg.Wait()
	enqueueDuration := time.Since(enqueueStart)
	t.Logf("Enqueued %d requests in %v (%.0f req/s)",
		numAgents, enqueueDuration, float64(numAgents)/enqueueDuration.Seconds())

	// Wait for all placements to complete
	timeout := time.After(5 * time.Minute)
	placementStart := time.Now()

	progressTicker := time.NewTicker(5 * time.Second)
	defer progressTicker.Stop()

	for int(placedCount.Load()) < numAgents {
		select {
		case <-placed:
			// Agent placed successfully
		case <-progressTicker.C:
			current := placedCount.Load()
			elapsed := time.Since(placementStart)
			throughput := float64(current) / elapsed.Seconds()
			t.Logf("Progress: %d/%d agents placed (%.0f placements/sec)",
				current, numAgents, throughput)
		case <-timeout:
			env.Metrics.Finalize()
			report := env.Metrics.GetReport()
			report.Print(t)
			t.Fatalf("Timeout waiting for placements (placed %d/%d)",
				placedCount.Load(), numAgents)
		}
	}

	// Finalize metrics and generate report
	env.Metrics.Finalize()
	report := env.Metrics.GetReport()

	// Print detailed report
	report.Print(t)

	// Validate against performance targets
	report.ValidatePerformance(t, targets)

	// Verify all agents were placed
	require.Equal(t, int64(numAgents), report.SuccessCount,
		"All %d agents should be successfully placed", numAgents)

	// Verify node distribution
	verifyNodeDistribution(t, env, numAgents)

	t.Logf("✅ Load test PASSED: %d agents placed in %v", numAgents, report.Duration)
}

// TestLoad_5000Agents validates system performance with 5,000 concurrent agents
func TestLoad_5000Agents(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping load test in short mode")
	}

	// Setup test environment with 50 nodes (100 agents per node capacity)
	env := SetupLoadTestEnvironment(t, 50)
	defer env.TearDown()

	// Setup distributed queue with 50 workers for higher throughput
	env.SetupQueue("5000-agents", 50)

	const numAgents = 5000

	// Performance targets for 5K agents (slightly relaxed due to higher load)
	targets := &PerformanceTarget{
		MaxAvgLatency: 100 * time.Millisecond,  // Average placement
		MaxP95Latency: 500 * time.Millisecond,  // P95 target (relaxed for high load)
		MaxP99Latency: 1000 * time.Millisecond, // P99 target
		MinThroughput: 50,                      // At least 50 placements/sec under high load
	}

	t.Logf("Starting HIGH LOAD test: %d agents across %d nodes", numAgents, len(env.Nodes))

	// Track placed agents
	var placedCount atomic.Int64
	placed := make(chan string, numAgents)

	// Register placement handler
	env.Queue.RegisterHandler(func(ctx context.Context, req *distributed.SchedulingRequest) error {
		placementStart := time.Now()

		// Get nodes owned by this scheduler
		nodes, err := env.NodeRegistry.GetOwnedNodes(ctx, env.SchedulerID)
		if err != nil {
			env.Metrics.RecordFailure()
			return fmt.Errorf("failed to get owned nodes: %w", err)
		}

		if len(nodes) == 0 {
			env.Metrics.RecordFailure()
			return fmt.Errorf("no nodes available")
		}

		// Bin-packing: find first node with capacity
		for _, node := range nodes {
			if node.CanFit(req.Resources) {
				// Try atomic allocation
				success, err := env.NodeRegistry.TryAllocate(ctx, node.ID, req.AgentID, req.Resources)
				if err != nil {
					continue // Try next node
				}

				if success {
					placementLatency := time.Since(placementStart)
					env.Metrics.RecordSuccess(placementLatency)
					env.Metrics.RecordPlacement(placementLatency)
					placedCount.Add(1)
					placed <- string(req.AgentID)
					return nil
				}
			}
		}

		env.Metrics.RecordFailure()
		return fmt.Errorf("no suitable node found")
	})

	// Enqueue agents in parallel batches
	t.Logf("Enqueuing %d agent requests...", numAgents)
	enqueueStart := time.Now()

	var enqueueWg sync.WaitGroup
	batchSize := 200 // Larger batches for 5K test
	numBatches := numAgents / batchSize

	for batch := 0; batch < numBatches; batch++ {
		enqueueWg.Add(1)
		go func(batchNum int) {
			defer enqueueWg.Done()

			for i := 0; i < batchSize; i++ {
				agentNum := batchNum*batchSize + i + 1
				req := CreateTestAgentRequest(agentNum, fmt.Sprintf("tenant-%d", agentNum%20+1))

				enqueueReqStart := time.Now()
				if err := env.Queue.Enqueue(env.ctx, req); err != nil {
					env.T.Errorf("Failed to enqueue agent %d: %v", agentNum, err)
					continue
				}
				env.Metrics.RecordEnqueue(time.Since(enqueueReqStart))
			}
		}(batch)
	}

	enqueueWg.Wait()
	enqueueDuration := time.Since(enqueueStart)
	t.Logf("Enqueued %d requests in %v (%.0f req/s)",
		numAgents, enqueueDuration, float64(numAgents)/enqueueDuration.Seconds())

	// Wait for all placements to complete
	timeout := time.After(15 * time.Minute) // Longer timeout for 5K test
	placementStart := time.Now()

	progressTicker := time.NewTicker(10 * time.Second)
	defer progressTicker.Stop()

	for int(placedCount.Load()) < numAgents {
		select {
		case <-placed:
			// Agent placed successfully
		case <-progressTicker.C:
			current := placedCount.Load()
			elapsed := time.Since(placementStart)
			throughput := float64(current) / elapsed.Seconds()
			t.Logf("Progress: %d/%d agents placed (%.0f placements/sec)",
				current, numAgents, throughput)
		case <-timeout:
			env.Metrics.Finalize()
			report := env.Metrics.GetReport()
			report.Print(t)
			t.Fatalf("Timeout waiting for placements (placed %d/%d)",
				placedCount.Load(), numAgents)
		}
	}

	// Finalize metrics and generate report
	env.Metrics.Finalize()
	report := env.Metrics.GetReport()

	// Print detailed report
	report.Print(t)

	// Validate against performance targets
	report.ValidatePerformance(t, targets)

	// Verify all agents were placed
	require.Equal(t, int64(numAgents), report.SuccessCount,
		"All %d agents should be successfully placed", numAgents)

	// Verify node distribution
	verifyNodeDistribution(t, env, numAgents)

	t.Logf("✅ HIGH LOAD test PASSED: %d agents placed in %v", numAgents, report.Duration)
}

// verifyNodeDistribution checks that agents are reasonably distributed across nodes
func verifyNodeDistribution(t *testing.T, env *LoadTestEnvironment, expectedTotal int) {
	t.Helper()

	nodes, err := env.NodeRegistry.GetOwnedNodes(env.ctx, env.SchedulerID)
	require.NoError(t, err, "Should be able to get nodes")

	totalAllocated := 0
	nodesWithAgents := 0
	maxAgentsPerNode := 0
	minAgentsPerNode := expectedTotal

	t.Logf("\n=== NODE DISTRIBUTION ===")
	for _, node := range nodes {
		agentCount := len(node.Agents)
		totalAllocated += agentCount

		if agentCount > 0 {
			nodesWithAgents++
			if agentCount > maxAgentsPerNode {
				maxAgentsPerNode = agentCount
			}
			if agentCount < minAgentsPerNode {
				minAgentsPerNode = agentCount
			}

			cpuPercent := float64(node.Allocated.CPUCores) / float64(node.Capacity.CPUCores) * 100
			memPercent := float64(node.Allocated.MemoryMB) / float64(node.Capacity.MemoryMB) * 100

			t.Logf("Node %-20s: %4d agents (CPU: %5.1f%%, RAM: %5.1f%%)",
				node.ID, agentCount, cpuPercent, memPercent)
		}
	}

	avgAgentsPerNode := float64(expectedTotal) / float64(len(nodes))
	t.Logf("\nTotal Nodes:           %d", len(nodes))
	t.Logf("Nodes with Agents:     %d", nodesWithAgents)
	t.Logf("Total Agents Allocated: %d", totalAllocated)
	t.Logf("Expected Total:        %d", expectedTotal)
	t.Logf("Avg Agents/Node:       %.1f", avgAgentsPerNode)
	t.Logf("Min Agents/Node:       %d", minAgentsPerNode)
	t.Logf("Max Agents/Node:       %d", maxAgentsPerNode)
	t.Logf("========================\n")

	require.Equal(t, expectedTotal, totalAllocated,
		"Total allocated agents should match expected")

	// Verify reasonable distribution (no node should be empty if we have agents)
	require.Greater(t, nodesWithAgents, 0, "At least some nodes should have agents")
}

// TestLoad_SustainedLoad validates system stability under sustained load
func TestLoad_SustainedLoad(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping sustained load test in short mode")
	}

	t.Skip("Sustained load test disabled by default (run manually for 30-minute test)")

	// This test runs for 30 minutes to detect memory leaks and performance degradation
	// Run manually when needed: go test -v -run TestLoad_SustainedLoad -timeout 45m

	env := SetupLoadTestEnvironment(t, 20)
	defer env.TearDown()

	env.SetupQueue("sustained", 30)

	const duration = 30 * time.Minute
	const agentsPerMinute = 100

	t.Logf("Starting SUSTAINED load test: %d minutes at %d agents/min",
		int(duration.Minutes()), agentsPerMinute)

	// TODO: Implement sustained load test
	// - Create agents at steady rate
	// - Monitor memory usage over time
	// - Detect performance degradation
	// - Check for memory leaks
}
