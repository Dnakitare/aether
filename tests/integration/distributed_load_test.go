package integration

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dnakitare/aether/internal/scheduler"
	"github.com/dnakitare/aether/internal/scheduler/distributed"
	"github.com/dnakitare/aether/pkg/api"
)

// TestDistributedScheduler_LoadTest_1000Agents tests placement of 1000 agents
func TestDistributedScheduler_LoadTest_1000Agents(t *testing.T) {
	skipIfNoDistributedInfra(t)

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelWarn, // Reduce log noise for load test
	}))

	ctx := context.Background()

	// 1. Create shard manager
	shardConfig := distributed.DefaultShardConfig()
	shardConfig.SchedulerID = "load-scheduler-1"
	shardConfig.InstanceID = "load-instance-1"
	shardConfig.Hostname = "localhost"
	shardConfig.VirtualNodes = 100
	shardConfig.HeartbeatInterval = 10 * time.Second

	shardManager, err := distributed.NewShardManager(logger, shardConfig)
	if err != nil {
		t.Fatalf("Failed to create shard manager: %v", err)
	}
	defer shardManager.Stop(ctx)

	if err := shardManager.Start(ctx); err != nil {
		t.Fatalf("Failed to start shard manager: %v", err)
	}

	time.Sleep(2 * time.Second)

	// 2. Create node registry
	nodeRegistryConfig := distributed.DefaultNodeRegistryConfig()
	nodeRegistryConfig.KeyPrefix = "aether:load"
	nodeRegistryConfig.NodeTTL = 120 * time.Second
	nodeRegistryConfig.RedisPassword = "redis_dev_password"

	nodeRegistry, err := distributed.NewNodeRegistry(logger, nodeRegistryConfig)
	if err != nil {
		t.Fatalf("Failed to create node registry: %v", err)
	}
	defer nodeRegistry.Close()

	// 3. Register 10 nodes with high capacity (each can hold 120 agents)
	for i := 1; i <= 10; i++ {
		node := &scheduler.Node{
			ID:   fmt.Sprintf("load-node-%03d", i),
			Name: fmt.Sprintf("Load Test Node %d", i),
			Labels: map[string]string{
				"region": "us-west-2",
				"zone":   fmt.Sprintf("us-west-2%c", 'a'+(i-1)%3),
			},
			Capacity: scheduler.Resources{
				CPUCores: 120000,  // 120 cores @ 1 core per agent
				MemoryMB: 245760,  // 240GB @ 2GB per agent = 120 agents
				DiskMB:   2097152, // 2TB
			},
			Allocated: scheduler.Resources{},
			Agents:    make(map[api.AgentID]*scheduler.AgentAllocation),
		}

		if err := nodeRegistry.RegisterNode(ctx, node, shardConfig.SchedulerID); err != nil {
			t.Fatalf("Failed to register node %d: %v", i, err)
		}
	}

	// 4. Create distributed queue
	queueConfig := distributed.DefaultQueueConfig()
	queueConfig.Topic = "load-test-1000"
	queueConfig.ConsumerGroup = "load-test-1000-group"
	queueConfig.NumWorkers = 20 // Increase workers for load test
	queueConfig.StartFromBeginning = true

	queue, err := distributed.NewDistributedQueue(logger, queueConfig)
	if err != nil {
		t.Fatalf("Failed to create distributed queue: %v", err)
	}
	defer queue.Stop(ctx)

	// 5. Register handler for placement with metrics
	var placedCount atomic.Int64
	var failedCount atomic.Int64
	var totalLatency atomic.Int64
	placed := make(chan string, 1000)

	queue.RegisterHandler(func(ctx context.Context, req *distributed.SchedulingRequest) error {
		startTime := time.Now()

		// Get nodes owned by this scheduler
		nodes, err := nodeRegistry.GetOwnedNodes(ctx, shardConfig.SchedulerID)
		if err != nil {
			failedCount.Add(1)
			return fmt.Errorf("failed to get owned nodes: %w", err)
		}

		if len(nodes) == 0 {
			failedCount.Add(1)
			return fmt.Errorf("no suitable node found")
		}

		// Simple bin-packing: pick first node with capacity
		for _, node := range nodes {
			if node.CanFit(req.Resources) {
				// Try atomic allocation
				success, err := nodeRegistry.TryAllocate(ctx, node.ID, req.AgentID, req.Resources)
				if err != nil {
					continue // Try next node
				}

				if success {
					latency := time.Since(startTime)
					totalLatency.Add(int64(latency))
					placedCount.Add(1)
					placed <- string(req.AgentID)
					return nil
				}
			}
		}

		failedCount.Add(1)
		return fmt.Errorf("no suitable node found after checking all nodes")
	})

	// Start queue consumer
	if err := queue.Start(ctx); err != nil {
		t.Fatalf("Failed to start queue: %v", err)
	}

	time.Sleep(2 * time.Second)

	// 6. Enqueue 1000 scheduling requests
	numRequests := 1000
	t.Logf("Enqueuing %d scheduling requests...", numRequests)

	enqueueStart := time.Now()

	// Use goroutines to enqueue in parallel
	var enqueueWg sync.WaitGroup
	enqueueErrors := make(chan error, numRequests)

	batchSize := 100
	for batch := 0; batch < numRequests/batchSize; batch++ {
		enqueueWg.Add(1)
		go func(batchNum int) {
			defer enqueueWg.Done()
			for i := 0; i < batchSize; i++ {
				agentNum := batchNum*batchSize + i + 1
				req := &scheduler.AgentRequest{
					Config: api.AgentConfig{
						ID:       api.AgentID(fmt.Sprintf("load-agent-%04d", agentNum)),
						TenantID: api.TenantID(fmt.Sprintf("tenant-%d", agentNum%10+1)),
						Name:     fmt.Sprintf("Load Test Agent %d", agentNum),
						Image:    "python:3.11",
					},
					Resources: scheduler.Resources{
						CPUCores: 1000,  // 1 core
						MemoryMB: 2048,  // 2GB
						DiskMB:   10240, // 10GB
					},
					Priority:  10,
					CreatedAt: time.Now(),
				}

				if err := queue.Enqueue(ctx, req); err != nil {
					enqueueErrors <- fmt.Errorf("failed to enqueue agent %d: %w", agentNum, err)
				}
			}
		}(batch)
	}

	enqueueWg.Wait()
	close(enqueueErrors)

	enqueueDuration := time.Since(enqueueStart)
	t.Logf("Enqueued %d requests in %v (%.0f req/s)", numRequests, enqueueDuration, float64(numRequests)/enqueueDuration.Seconds())

	// Check for enqueue errors
	errCount := 0
	for err := range enqueueErrors {
		t.Logf("Enqueue error: %v", err)
		errCount++
	}
	if errCount > 0 {
		t.Fatalf("%d enqueue errors occurred", errCount)
	}

	// 7. Wait for all placements to complete
	timeout := time.After(2 * time.Minute)
	placementStart := time.Now()

	for int(placedCount.Load()) < numRequests {
		select {
		case <-placed:
			current := placedCount.Load()
			if current%100 == 0 {
				elapsed := time.Since(placementStart)
				throughput := float64(current) / elapsed.Seconds()
				t.Logf("Placed %d/%d agents (%.0f placements/sec)", current, numRequests, throughput)
			}
		case <-timeout:
			t.Fatalf("Timeout waiting for placements (placed %d/%d, failed %d)",
				placedCount.Load(), numRequests, failedCount.Load())
		}
	}

	placementDuration := time.Since(placementStart)

	// 8. Calculate metrics
	successCount := int(placedCount.Load())
	failCount := int(failedCount.Load())
	throughput := float64(successCount) / placementDuration.Seconds()
	avgLatency := time.Duration(totalLatency.Load() / int64(successCount))

	t.Logf("\n=== Load Test Results ===")
	t.Logf("Total Requests: %d", numRequests)
	t.Logf("Successfully Placed: %d", successCount)
	t.Logf("Failed: %d", failCount)
	t.Logf("Placement Duration: %v", placementDuration)
	t.Logf("Throughput: %.2f placements/sec", throughput)
	t.Logf("Average Latency: %v", avgLatency)

	// 9. Verify node distribution
	nodes, err := nodeRegistry.GetOwnedNodes(ctx, shardConfig.SchedulerID)
	if err != nil {
		t.Fatalf("Failed to get nodes: %v", err)
	}

	totalAllocated := 0
	t.Logf("\n=== Node Distribution ===")
	for _, node := range nodes {
		agentCount := len(node.Agents)
		totalAllocated += agentCount
		if agentCount > 0 {
			t.Logf("Node %s: %d agents (%.1f%% CPU, %.1f%% RAM)",
				node.ID, agentCount,
				float64(node.Allocated.CPUCores)/float64(node.Capacity.CPUCores)*100,
				float64(node.Allocated.MemoryMB)/float64(node.Capacity.MemoryMB)*100)
		}
	}

	if totalAllocated != numRequests {
		t.Errorf("Total allocated = %d, want %d", totalAllocated, numRequests)
	}

	// 10. Verify performance targets
	if throughput < 100 {
		t.Errorf("Throughput = %.2f placements/sec, want >= 100", throughput)
	}

	if avgLatency > 100*time.Millisecond {
		t.Errorf("Average latency = %v, want <= 100ms", avgLatency)
	}

	t.Logf("\n=== SUCCESS ===")
	t.Logf("Load test completed: %d agents placed in %v", successCount, placementDuration)
}
