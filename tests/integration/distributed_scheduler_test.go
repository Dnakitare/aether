package integration

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/dnakitare/aether/internal/scheduler"
	"github.com/dnakitare/aether/internal/scheduler/distributed"
	"github.com/dnakitare/aether/pkg/api"
)

// Skip these tests if distributed infrastructure is not available
func skipIfNoDistributedInfra(t *testing.T) {
	if os.Getenv("DISTRIBUTED_INFRA_AVAILABLE") != "true" {
		t.Skip("Skipping test: distributed infrastructure not available (set DISTRIBUTED_INFRA_AVAILABLE=true to run)")
	}
}

// TestDistributedScheduler_EndToEnd tests the full distributed scheduler flow
func TestDistributedScheduler_EndToEnd(t *testing.T) {
	skipIfNoDistributedInfra(t)

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	ctx := context.Background()

	// 1. Create shard manager
	shardConfig := distributed.DefaultShardConfig()
	shardConfig.SchedulerID = "test-scheduler-1"
	shardConfig.InstanceID = "test-instance-1"
	shardConfig.Hostname = "localhost"
	shardConfig.VirtualNodes = 10
	shardConfig.HeartbeatInterval = 5 * time.Second

	shardManager, err := distributed.NewShardManager(logger, shardConfig)
	if err != nil {
		t.Fatalf("Failed to create shard manager: %v", err)
	}
	defer shardManager.Stop(ctx)

	if err := shardManager.Start(ctx); err != nil {
		t.Fatalf("Failed to start shard manager: %v", err)
	}

	// Give shard manager time to register
	time.Sleep(2 * time.Second)

	// 2. Create node registry
	nodeRegistryConfig := distributed.DefaultNodeRegistryConfig()
	nodeRegistryConfig.KeyPrefix = "aether:test"
	nodeRegistryConfig.NodeTTL = 30 * time.Second
	nodeRegistryConfig.RedisPassword = "redis_dev_password"

	nodeRegistry, err := distributed.NewNodeRegistry(logger, nodeRegistryConfig)
	if err != nil {
		t.Fatalf("Failed to create node registry: %v", err)
	}
	defer nodeRegistry.Close()

	// 3. Register test nodes
	for i := 1; i <= 3; i++ {
		node := &scheduler.Node{
			ID:   fmt.Sprintf("node-%03d", i),
			Name: fmt.Sprintf("Test Node %d", i),
			Labels: map[string]string{
				"region": "us-west-2",
				"zone":   fmt.Sprintf("us-west-2%c", 'a'+i-1),
			},
			Capacity: scheduler.Resources{
				CPUCores: 8000,
				MemoryMB: 16384,
				DiskMB:   102400,
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
	queueConfig.Topic = "test-scheduling-e2e"
	queueConfig.ConsumerGroup = "test-e2e-group"
	queueConfig.NumWorkers = 5
	queueConfig.StartFromBeginning = true // For testing, read from beginning

	queue, err := distributed.NewDistributedQueue(logger, queueConfig)
	if err != nil {
		t.Fatalf("Failed to create distributed queue: %v", err)
	}
	defer queue.Stop(ctx)

	// 5. Register handler for placement
	placed := make(chan string, 10)
	queue.RegisterHandler(func(ctx context.Context, req *distributed.SchedulingRequest) error {
		logger.InfoContext(ctx, "processing scheduling request",
			"agent_id", req.AgentID,
			"tenant_id", req.TenantID,
		)

		// Get nodes owned by this scheduler
		nodes, err := nodeRegistry.GetOwnedNodes(ctx, shardConfig.SchedulerID)
		if err != nil {
			return fmt.Errorf("failed to get owned nodes: %w", err)
		}

		if len(nodes) == 0 {
			return fmt.Errorf("no suitable node found")
		}

		// Simple placement: pick first node with capacity
		for _, node := range nodes {
			if node.CanFit(req.Resources) {
				// Try atomic allocation
				success, err := nodeRegistry.TryAllocate(ctx, node.ID, req.AgentID, req.Resources)
				if err != nil {
					return fmt.Errorf("allocation failed: %w", err)
				}

				if success {
					logger.InfoContext(ctx, "agent placed",
						"agent_id", req.AgentID,
						"node_id", node.ID,
					)
					placed <- string(req.AgentID)
					return nil
				}
			}
		}

		return fmt.Errorf("no suitable node found")
	})

	// Start queue consumer
	if err := queue.Start(ctx); err != nil {
		t.Fatalf("Failed to start queue: %v", err)
	}

	// 6. Enqueue scheduling requests
	numRequests := 5
	for i := 1; i <= numRequests; i++ {
		req := &scheduler.AgentRequest{
			Config: api.AgentConfig{
				ID:       api.AgentID(fmt.Sprintf("agent-%03d", i)),
				TenantID: api.TenantID(fmt.Sprintf("tenant-%d", i%3+1)),
				Name:     fmt.Sprintf("Test Agent %d", i),
				Image:    "python:3.11",
			},
			Resources: scheduler.Resources{
				CPUCores: 2000,
				MemoryMB: 2048,
				DiskMB:   10240,
			},
			Priority:  10,
			CreatedAt: time.Now(),
		}

		if err := queue.Enqueue(ctx, req); err != nil {
			t.Fatalf("Failed to enqueue request %d: %v", i, err)
		}
	}

	// 7. Wait for all placements to complete
	timeout := time.After(30 * time.Second)
	placedCount := 0

	for placedCount < numRequests {
		select {
		case agentID := <-placed:
			placedCount++
			t.Logf("Placed agent %d/%d: %s", placedCount, numRequests, agentID)
		case <-timeout:
			t.Fatalf("Timeout waiting for placements (placed %d/%d)", placedCount, numRequests)
		}
	}

	// 8. Verify all agents were placed
	nodes, err := nodeRegistry.GetOwnedNodes(ctx, shardConfig.SchedulerID)
	if err != nil {
		t.Fatalf("Failed to get nodes: %v", err)
	}

	totalAllocated := 0
	for _, node := range nodes {
		totalAllocated += len(node.Agents)
		t.Logf("Node %s: %d agents, utilization %.1f%%",
			node.ID, len(node.Agents), node.UtilizationPercent())
	}

	if totalAllocated != numRequests {
		t.Errorf("Total allocated = %d, want %d", totalAllocated, numRequests)
	}

	t.Logf("Successfully placed %d agents across %d nodes", totalAllocated, len(nodes))
}

// TestDistributedScheduler_MultiScheduler tests multiple schedulers working together
func TestDistributedScheduler_MultiScheduler(t *testing.T) {
	skipIfNoDistributedInfra(t)

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	ctx := context.Background()

	// Create 2 schedulers
	schedulers := make([]*distributed.ShardManager, 2)
	for i := 0; i < 2; i++ {
		config := distributed.DefaultShardConfig()
		config.SchedulerID = fmt.Sprintf("scheduler-%d", i+1)
		config.InstanceID = fmt.Sprintf("instance-%d", i+1)
		config.Hostname = "localhost"
		config.VirtualNodes = 10
		config.HeartbeatInterval = 5 * time.Second

		sm, err := distributed.NewShardManager(logger, config)
		if err != nil {
			t.Fatalf("Failed to create scheduler %d: %v", i+1, err)
		}
		defer sm.Stop(ctx)

		if err := sm.Start(ctx); err != nil {
			t.Fatalf("Failed to start scheduler %d: %v", i+1, err)
		}

		schedulers[i] = sm
	}

	// Give time for discovery
	time.Sleep(3 * time.Second)

	// Verify both schedulers see each other
	members1 := schedulers[0].Members()
	members2 := schedulers[1].Members()

	if len(members1) != 2 {
		t.Errorf("Scheduler 1 members = %d, want 2", len(members1))
	}

	if len(members2) != 2 {
		t.Errorf("Scheduler 2 members = %d, want 2", len(members2))
	}

	// Test consistent hashing - same node should map to same scheduler
	testNodeID := "node-test-001"
	scheduler1Assignment := schedulers[0].GetScheduler(testNodeID)
	scheduler2Assignment := schedulers[1].GetScheduler(testNodeID)

	if scheduler1Assignment != scheduler2Assignment {
		t.Errorf("Inconsistent assignment: scheduler1=%s, scheduler2=%s",
			scheduler1Assignment, scheduler2Assignment)
	}

	t.Logf("Consistent assignment verified: node %s -> %s", testNodeID, scheduler1Assignment)

	// Test ownership
	isOwner1 := schedulers[0].IsOwner(testNodeID)
	isOwner2 := schedulers[1].IsOwner(testNodeID)

	// Exactly one scheduler should own the node
	if isOwner1 == isOwner2 {
		t.Errorf("Ownership conflict: both=%v (should be exclusive)", isOwner1)
	}

	t.Logf("Ownership verified: scheduler-1=%v, scheduler-2=%v", isOwner1, isOwner2)
}

// TestDistributedScheduler_FailoverScenario tests scheduler failover
func TestDistributedScheduler_FailoverScenario(t *testing.T) {
	skipIfNoDistributedInfra(t)

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	ctx := context.Background()

	// Start scheduler 1
	config1 := distributed.DefaultShardConfig()
	config1.SchedulerID = "scheduler-failover-1"
	config1.InstanceID = "instance-failover-1"
	config1.Hostname = "localhost"
	config1.VirtualNodes = 10

	sm1, err := distributed.NewShardManager(logger, config1)
	if err != nil {
		t.Fatalf("Failed to create scheduler 1: %v", err)
	}

	if err := sm1.Start(ctx); err != nil {
		t.Fatalf("Failed to start scheduler 1: %v", err)
	}

	time.Sleep(2 * time.Second)

	// Start scheduler 2
	config2 := distributed.DefaultShardConfig()
	config2.SchedulerID = "scheduler-failover-2"
	config2.InstanceID = "instance-failover-2"
	config2.Hostname = "localhost"
	config2.VirtualNodes = 10

	sm2, err := distributed.NewShardManager(logger, config2)
	if err != nil {
		t.Fatalf("Failed to create scheduler 2: %v", err)
	}
	defer sm2.Stop(ctx)

	if err := sm2.Start(ctx); err != nil {
		t.Fatalf("Failed to start scheduler 2: %v", err)
	}

	time.Sleep(2 * time.Second)

	// Both schedulers active
	if len(sm2.Members()) != 2 {
		t.Errorf("Expected 2 schedulers, got %d", len(sm2.Members()))
	}

	testNodeID := "node-failover-test"
	initialOwner := sm2.GetScheduler(testNodeID)
	t.Logf("Initial owner: %s", initialOwner)

	// Stop scheduler 1 (simulate failure)
	t.Log("Stopping scheduler 1 (simulating failure)...")
	if err := sm1.Stop(ctx); err != nil {
		t.Fatalf("Failed to stop scheduler 1: %v", err)
	}

	// Wait for TTL expiry and discovery
	time.Sleep(35 * time.Second)

	// Only scheduler 2 should remain
	members := sm2.Members()
	if len(members) != 1 {
		t.Errorf("After failover, expected 1 scheduler, got %d", len(members))
	}

	// Node should now be owned by scheduler 2
	newOwner := sm2.GetScheduler(testNodeID)
	t.Logf("New owner after failover: %s", newOwner)

	if newOwner != "scheduler-failover-2" {
		t.Errorf("Expected scheduler-failover-2 to own node, got %s", newOwner)
	}

	t.Log("Failover successful: nodes reassigned to scheduler-2")
}
