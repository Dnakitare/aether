package scheduler_test

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/dnakitare/aether/internal/scheduler"
	"github.com/dnakitare/aether/pkg/api"
)

// ============================================================================
// Queue Concurrency Tests
// ============================================================================

func TestQueueConcurrentEnqueue(t *testing.T) {
	t.Run("multiple_producers", func(t *testing.T) {
		queue := scheduler.NewQueue()

		const numProducers = 10
		const itemsPerProducer = 100

		var wg sync.WaitGroup
		wg.Add(numProducers)

		for p := 0; p < numProducers; p++ {
			go func(producerID int) {
				defer wg.Done()
				for i := 0; i < itemsPerProducer; i++ {
					req := &scheduler.AgentRequest{
						Config: api.AgentConfig{
							ID:       api.AgentID(fmt.Sprintf("agent-%d-%d", producerID, i)),
							TenantID: api.TenantID("tenant-1"),
							Name:     "Test Agent",
						},
						Resources: scheduler.Resources{
							CPUCores: 1000,
							MemoryMB: 512,
							DiskMB:   10240,
						},
						Priority:  producerID, // Different priorities
						CreatedAt: time.Now(),
					}
					queue.Enqueue(req)
				}
			}(p)
		}

		wg.Wait()

		expectedLength := numProducers * itemsPerProducer
		if queue.Len() != expectedLength {
			t.Errorf("Queue length = %d, want %d", queue.Len(), expectedLength)
		}
	})

	t.Run("concurrent_enqueue_dequeue", func(t *testing.T) {
		queue := scheduler.NewQueue()

		const numOperations = 1000
		var enqueued, dequeued sync.Map

		var wg sync.WaitGroup
		wg.Add(2)

		// Producer
		go func() {
			defer wg.Done()
			for i := 0; i < numOperations; i++ {
				req := &scheduler.AgentRequest{
					Config: api.AgentConfig{
						ID:       api.AgentID(fmt.Sprintf("agent-%d", i)),
						TenantID: api.TenantID("tenant-1"),
					},
					Resources: scheduler.Resources{CPUCores: 1000, MemoryMB: 512},
					Priority:  i % 10,
					CreatedAt: time.Now(),
				}
				queue.Enqueue(req)
				enqueued.Store(req.Config.ID, true)
			}
		}()

		// Consumer
		go func() {
			defer wg.Done()
			for i := 0; i < numOperations; i++ {
				for {
					req := queue.Dequeue()
					if req != nil {
						dequeued.Store(req.Config.ID, true)
						break
					}
					time.Sleep(1 * time.Millisecond)
				}
			}
		}()

		wg.Wait()

		// Verify all items were processed
		enqueued.Range(func(key, value interface{}) bool {
			if _, ok := dequeued.Load(key); !ok {
				t.Errorf("Agent %s was enqueued but not dequeued", key)
			}
			return true
		})

		if queue.Len() != 0 {
			t.Errorf("Queue should be empty, but has %d items", queue.Len())
		}
	})
}

func TestQueuePriorityOrdering(t *testing.T) {
	t.Run("higher_priority_first", func(t *testing.T) {
		queue := scheduler.NewQueue()

		// Add items with different priorities
		for priority := 1; priority <= 10; priority++ {
			req := &scheduler.AgentRequest{
				Config: api.AgentConfig{
					ID:       api.AgentID(fmt.Sprintf("agent-%d", priority)),
					TenantID: api.TenantID("tenant-1"),
				},
				Resources: scheduler.Resources{CPUCores: 1000, MemoryMB: 512},
				Priority:  priority,
				CreatedAt: time.Now(),
			}
			queue.Enqueue(req)
		}

		// Dequeue should return items from highest to lowest priority
		for expectedPriority := 10; expectedPriority >= 1; expectedPriority-- {
			req := queue.Dequeue()
			if req == nil {
				t.Fatalf("Expected request with priority %d, got nil", expectedPriority)
			}
			if req.Priority != expectedPriority {
				t.Errorf("Priority = %d, want %d", req.Priority, expectedPriority)
			}
		}
	})

	t.Run("fifo_for_same_priority", func(t *testing.T) {
		queue := scheduler.NewQueue()

		const samePriority = 5
		const numItems = 100

		// Add items with same priority at different times
		for i := 0; i < numItems; i++ {
			req := &scheduler.AgentRequest{
				Config: api.AgentConfig{
					ID:       api.AgentID(fmt.Sprintf("agent-%d", i)),
					TenantID: api.TenantID("tenant-1"),
				},
				Resources: scheduler.Resources{CPUCores: 1000, MemoryMB: 512},
				Priority:  samePriority,
				CreatedAt: time.Now().Add(time.Duration(i) * time.Microsecond),
			}
			queue.Enqueue(req)
			time.Sleep(1 * time.Microsecond) // Ensure different timestamps
		}

		// Verify FIFO order
		for i := 0; i < numItems; i++ {
			req := queue.Dequeue()
			if req == nil {
				t.Fatalf("Expected agent-%d, got nil", i)
			}
			expectedID := api.AgentID(fmt.Sprintf("agent-%d", i))
			if req.Config.ID != expectedID {
				t.Errorf("Agent ID = %s, want %s", req.Config.ID, expectedID)
			}
		}
	})
}

func TestQueueRemove(t *testing.T) {
	t.Run("remove_from_middle", func(t *testing.T) {
		queue := scheduler.NewQueue()

		// Add multiple items with same priority to ensure deterministic order
		ids := []api.AgentID{"agent-1", "agent-2", "agent-3", "agent-4", "agent-5"}
		now := time.Now()
		for i, id := range ids {
			req := &scheduler.AgentRequest{
				Config:    api.AgentConfig{ID: id, TenantID: "tenant-1"},
				Resources: scheduler.Resources{CPUCores: 1000, MemoryMB: 512},
				Priority:  10, // Same priority for all
				CreatedAt: now.Add(time.Duration(i) * time.Microsecond),
			}
			queue.Enqueue(req)
		}

		// Remove middle item (agent-3)
		removed := queue.Remove(api.AgentID("agent-3"))
		if !removed {
			t.Error("Remove returned false, expected true")
		}

		if queue.Len() != 4 {
			t.Errorf("Queue length = %d, want 4", queue.Len())
		}

		// Verify agent-3 is gone and others are present
		remaining := make(map[api.AgentID]bool)
		for queue.Len() > 0 {
			req := queue.Dequeue()
			if req.Config.ID == "agent-3" {
				t.Error("agent-3 should have been removed but was found in queue")
			}
			remaining[req.Config.ID] = true
		}

		// Verify we got exactly the 4 agents we expect
		expected := []api.AgentID{"agent-1", "agent-2", "agent-4", "agent-5"}
		for _, id := range expected {
			if !remaining[id] {
				t.Errorf("Expected agent %s in queue but it was not found", id)
			}
		}
	})

	t.Run("remove_nonexistent", func(t *testing.T) {
		queue := scheduler.NewQueue()

		req := &scheduler.AgentRequest{
			Config:    api.AgentConfig{ID: "agent-1", TenantID: "tenant-1"},
			Resources: scheduler.Resources{CPUCores: 1000, MemoryMB: 512},
			Priority:  5,
			CreatedAt: time.Now(),
		}
		queue.Enqueue(req)

		removed := queue.Remove("nonexistent")
		if removed {
			t.Error("Remove returned true for nonexistent agent")
		}

		if queue.Len() != 1 {
			t.Errorf("Queue length = %d, want 1", queue.Len())
		}
	})

	t.Run("concurrent_remove_and_dequeue", func(t *testing.T) {
		queue := scheduler.NewQueue()

		const numItems = 100
		for i := 0; i < numItems; i++ {
			req := &scheduler.AgentRequest{
				Config:    api.AgentConfig{ID: api.AgentID(fmt.Sprintf("agent-%d", i)), TenantID: "tenant-1"},
				Resources: scheduler.Resources{CPUCores: 1000, MemoryMB: 512},
				Priority:  i,
				CreatedAt: time.Now(),
			}
			queue.Enqueue(req)
		}

		var wg sync.WaitGroup
		wg.Add(2)

		// Goroutine 1: Remove even numbered agents
		go func() {
			defer wg.Done()
			for i := 0; i < numItems; i += 2 {
				queue.Remove(api.AgentID(fmt.Sprintf("agent-%d", i)))
			}
		}()

		// Goroutine 2: Dequeue items
		dequeued := 0
		go func() {
			defer wg.Done()
			for queue.Len() > 0 {
				if queue.Dequeue() != nil {
					dequeued++
				}
				time.Sleep(1 * time.Millisecond)
			}
		}()

		wg.Wait()

		// Should have processed all items (either removed or dequeued)
		// No exact count since it's a race, but queue should be empty
		if queue.Len() != 0 {
			t.Errorf("Queue should be empty, has %d items", queue.Len())
		}
	})
}

// ============================================================================
// Placement Strategy Tests
// ============================================================================

func TestPlacementBinPacking(t *testing.T) {
	t.Run("selects_most_utilized_node", func(t *testing.T) {
		placer := scheduler.NewPlacer(scheduler.BinPacking)

		// Create nodes with different utilization levels
		node1 := &scheduler.Node{
			ID:   "node-1",
			Name: "Low Utilization",
			Capacity: scheduler.Resources{
				CPUCores: 4000,
				MemoryMB: 8192,
				DiskMB:   102400,
			},
			Allocated: scheduler.Resources{
				CPUCores: 1000, // 25% utilized
				MemoryMB: 2048,
			},
			Agents: make(map[api.AgentID]*scheduler.AgentAllocation),
		}

		node2 := &scheduler.Node{
			ID:   "node-2",
			Name: "High Utilization",
			Capacity: scheduler.Resources{
				CPUCores: 4000,
				MemoryMB: 8192,
				DiskMB:   102400,
			},
			Allocated: scheduler.Resources{
				CPUCores: 3000, // 75% utilized
				MemoryMB: 6144,
			},
			Agents: make(map[api.AgentID]*scheduler.AgentAllocation),
		}

		req := &scheduler.AgentRequest{
			Config:    api.AgentConfig{ID: "agent-1", TenantID: "tenant-1"},
			Resources: scheduler.Resources{CPUCores: 500, MemoryMB: 512},
		}

		node, err := placer.SelectNode(req, []*scheduler.Node{node1, node2})
		if err != nil {
			t.Fatalf("SelectNode failed: %v", err)
		}

		// Should select node2 (higher utilization)
		if node.ID != "node-2" {
			t.Errorf("Selected node = %s, want node-2 (bin-packing should select highest utilization)", node.ID)
		}
	})
}

func TestPlacementSpread(t *testing.T) {
	t.Run("selects_least_utilized_node", func(t *testing.T) {
		placer := scheduler.NewPlacer(scheduler.Spread)

		node1 := &scheduler.Node{
			ID:        "node-1",
			Capacity:  scheduler.Resources{CPUCores: 4000, MemoryMB: 8192, DiskMB: 102400},
			Allocated: scheduler.Resources{CPUCores: 1000, MemoryMB: 2048}, // 25% utilized
			Agents:    make(map[api.AgentID]*scheduler.AgentAllocation),
		}

		node2 := &scheduler.Node{
			ID:        "node-2",
			Capacity:  scheduler.Resources{CPUCores: 4000, MemoryMB: 8192, DiskMB: 102400},
			Allocated: scheduler.Resources{CPUCores: 3000, MemoryMB: 6144}, // 75% utilized
			Agents:    make(map[api.AgentID]*scheduler.AgentAllocation),
		}

		req := &scheduler.AgentRequest{
			Config:    api.AgentConfig{ID: "agent-1", TenantID: "tenant-1"},
			Resources: scheduler.Resources{CPUCores: 500, MemoryMB: 512},
		}

		node, err := placer.SelectNode(req, []*scheduler.Node{node1, node2})
		if err != nil {
			t.Fatalf("SelectNode failed: %v", err)
		}

		// Should select node1 (lower utilization)
		if node.ID != "node-1" {
			t.Errorf("Selected node = %s, want node-1 (spread should select lowest utilization)", node.ID)
		}
	})
}

func TestPlacementBestFit(t *testing.T) {
	t.Run("selects_node_with_minimal_waste", func(t *testing.T) {
		placer := scheduler.NewPlacer(scheduler.BestFit)

		// Node1: Will have 500 CPU and 512 MB left (minimal waste)
		node1 := &scheduler.Node{
			ID:        "node-1",
			Capacity:  scheduler.Resources{CPUCores: 2000, MemoryMB: 2048, DiskMB: 102400},
			Allocated: scheduler.Resources{CPUCores: 500, MemoryMB: 512},
			Agents:    make(map[api.AgentID]*scheduler.AgentAllocation),
		}

		// Node2: Will have 2500 CPU and 6144 MB left (more waste)
		node2 := &scheduler.Node{
			ID:        "node-2",
			Capacity:  scheduler.Resources{CPUCores: 4000, MemoryMB: 8192, DiskMB: 102400},
			Allocated: scheduler.Resources{CPUCores: 500, MemoryMB: 1024},
			Agents:    make(map[api.AgentID]*scheduler.AgentAllocation),
		}

		req := &scheduler.AgentRequest{
			Config:    api.AgentConfig{ID: "agent-1", TenantID: "tenant-1"},
			Resources: scheduler.Resources{CPUCores: 1000, MemoryMB: 1024},
		}

		node, err := placer.SelectNode(req, []*scheduler.Node{node1, node2})
		if err != nil {
			t.Fatalf("SelectNode failed: %v", err)
		}

		// Should select node1 (less waste after allocation)
		if node.ID != "node-1" {
			t.Errorf("Selected node = %s, want node-1 (best-fit should minimize waste)", node.ID)
		}
	})
}

func TestPlacementConstraints(t *testing.T) {
	t.Run("node_selector_matching", func(t *testing.T) {
		placer := scheduler.NewPlacer(scheduler.BinPacking)

		node1 := &scheduler.Node{
			ID:       "node-1",
			Labels:   map[string]string{"region": "us-west", "tier": "premium"},
			Capacity: scheduler.Resources{CPUCores: 4000, MemoryMB: 8192, DiskMB: 102400},
			Agents:   make(map[api.AgentID]*scheduler.AgentAllocation),
		}

		node2 := &scheduler.Node{
			ID:       "node-2",
			Labels:   map[string]string{"region": "us-east", "tier": "standard"},
			Capacity: scheduler.Resources{CPUCores: 4000, MemoryMB: 8192, DiskMB: 102400},
			Agents:   make(map[api.AgentID]*scheduler.AgentAllocation),
		}

		req := &scheduler.AgentRequest{
			Config:    api.AgentConfig{ID: "agent-1", TenantID: "tenant-1"},
			Resources: scheduler.Resources{CPUCores: 1000, MemoryMB: 1024},
			Constraints: scheduler.SchedulingConstraints{
				NodeSelector: map[string]string{"region": "us-west"},
			},
		}

		node, err := placer.SelectNode(req, []*scheduler.Node{node1, node2})
		if err != nil {
			t.Fatalf("SelectNode failed: %v", err)
		}

		if node.ID != "node-1" {
			t.Errorf("Selected node = %s, want node-1 (should match node selector)", node.ID)
		}
	})

	t.Run("node_selector_no_match", func(t *testing.T) {
		placer := scheduler.NewPlacer(scheduler.BinPacking)

		node := &scheduler.Node{
			ID:       "node-1",
			Labels:   map[string]string{"region": "us-east"},
			Capacity: scheduler.Resources{CPUCores: 4000, MemoryMB: 8192, DiskMB: 102400},
			Agents:   make(map[api.AgentID]*scheduler.AgentAllocation),
		}

		req := &scheduler.AgentRequest{
			Config:    api.AgentConfig{ID: "agent-1", TenantID: "tenant-1"},
			Resources: scheduler.Resources{CPUCores: 1000, MemoryMB: 1024},
			Constraints: scheduler.SchedulingConstraints{
				NodeSelector: map[string]string{"region": "us-west"},
			},
		}

		_, err := placer.SelectNode(req, []*scheduler.Node{node})
		if err == nil {
			t.Error("Expected error when no nodes match selector, got nil")
		}
	})

	t.Run("exclusive_node_constraint", func(t *testing.T) {
		placer := scheduler.NewPlacer(scheduler.BinPacking)

		// Node with existing agents
		node1 := &scheduler.Node{
			ID:       "node-1",
			Capacity: scheduler.Resources{CPUCores: 4000, MemoryMB: 8192, DiskMB: 102400},
			Agents: map[api.AgentID]*scheduler.AgentAllocation{
				"existing-agent": {
					AgentID:   "existing-agent",
					TenantID:  "tenant-1",
					Resources: scheduler.Resources{CPUCores: 1000, MemoryMB: 1024},
				},
			},
		}

		// Empty node
		node2 := &scheduler.Node{
			ID:       "node-2",
			Capacity: scheduler.Resources{CPUCores: 4000, MemoryMB: 8192, DiskMB: 102400},
			Agents:   make(map[api.AgentID]*scheduler.AgentAllocation),
		}

		req := &scheduler.AgentRequest{
			Config:    api.AgentConfig{ID: "agent-1", TenantID: "tenant-2"},
			Resources: scheduler.Resources{CPUCores: 1000, MemoryMB: 1024},
			Constraints: scheduler.SchedulingConstraints{
				RequireExclusiveNode: true,
			},
		}

		node, err := placer.SelectNode(req, []*scheduler.Node{node1, node2})
		if err != nil {
			t.Fatalf("SelectNode failed: %v", err)
		}

		if node.ID != "node-2" {
			t.Errorf("Selected node = %s, want node-2 (should select empty node for exclusive constraint)", node.ID)
		}
	})

	t.Run("anti_affinity_tenant", func(t *testing.T) {
		placer := scheduler.NewPlacer(scheduler.BinPacking)

		// Node with tenant-1 agents
		node1 := &scheduler.Node{
			ID:       "node-1",
			Capacity: scheduler.Resources{CPUCores: 4000, MemoryMB: 8192, DiskMB: 102400},
			Agents: map[api.AgentID]*scheduler.AgentAllocation{
				"agent-1": {
					AgentID:   "agent-1",
					TenantID:  "tenant-1",
					Resources: scheduler.Resources{CPUCores: 1000, MemoryMB: 1024},
				},
			},
		}

		// Node with tenant-2 agents
		node2 := &scheduler.Node{
			ID:       "node-2",
			Capacity: scheduler.Resources{CPUCores: 4000, MemoryMB: 8192, DiskMB: 102400},
			Agents: map[api.AgentID]*scheduler.AgentAllocation{
				"agent-2": {
					AgentID:   "agent-2",
					TenantID:  "tenant-2",
					Resources: scheduler.Resources{CPUCores: 1000, MemoryMB: 1024},
				},
			},
		}

		req := &scheduler.AgentRequest{
			Config:    api.AgentConfig{ID: "agent-3", TenantID: "tenant-1"},
			Resources: scheduler.Resources{CPUCores: 1000, MemoryMB: 1024},
			Constraints: scheduler.SchedulingConstraints{
				AntiAffinityTenant: true,
			},
		}

		node, err := placer.SelectNode(req, []*scheduler.Node{node1, node2})
		if err != nil {
			t.Fatalf("SelectNode failed: %v", err)
		}

		if node.ID != "node-2" {
			t.Errorf("Selected node = %s, want node-2 (should avoid nodes with same tenant)", node.ID)
		}
	})
}

// ============================================================================
// Node Management Tests
// ============================================================================

func TestNodeAllocationDeallocation(t *testing.T) {
	t.Run("allocate_and_deallocate", func(t *testing.T) {
		node := &scheduler.Node{
			ID:        "node-1",
			Capacity:  scheduler.Resources{CPUCores: 4000, MemoryMB: 8192, DiskMB: 102400},
			Allocated: scheduler.Resources{},
			Agents:    make(map[api.AgentID]*scheduler.AgentAllocation),
		}

		resources := scheduler.Resources{CPUCores: 1000, MemoryMB: 1024, DiskMB: 10240}

		// Allocate
		node.Allocate("agent-1", "tenant-1", resources)

		if node.Allocated.CPUCores != 1000 {
			t.Errorf("Allocated CPU = %d, want 1000", node.Allocated.CPUCores)
		}
		if node.Allocated.MemoryMB != 1024 {
			t.Errorf("Allocated Memory = %d, want 1024", node.Allocated.MemoryMB)
		}
		if node.AgentCount() != 1 {
			t.Errorf("Agent count = %d, want 1", node.AgentCount())
		}

		// Deallocate
		node.Deallocate("agent-1")

		if node.Allocated.CPUCores != 0 {
			t.Errorf("Allocated CPU after deallocation = %d, want 0", node.Allocated.CPUCores)
		}
		if node.Allocated.MemoryMB != 0 {
			t.Errorf("Allocated Memory after deallocation = %d, want 0", node.Allocated.MemoryMB)
		}
		if node.AgentCount() != 0 {
			t.Errorf("Agent count after deallocation = %d, want 0", node.AgentCount())
		}
	})

	t.Run("concurrent_allocations", func(t *testing.T) {
		node := &scheduler.Node{
			ID:        "node-1",
			Capacity:  scheduler.Resources{CPUCores: 100000, MemoryMB: 102400, DiskMB: 1024000},
			Allocated: scheduler.Resources{},
			Agents:    make(map[api.AgentID]*scheduler.AgentAllocation),
		}

		const numAgents = 100
		var wg sync.WaitGroup
		wg.Add(numAgents)

		for i := 0; i < numAgents; i++ {
			go func(id int) {
				defer wg.Done()
				agentID := api.AgentID(fmt.Sprintf("agent-%d", id))
				resources := scheduler.Resources{CPUCores: 100, MemoryMB: 256, DiskMB: 1024}
				node.Allocate(agentID, "tenant-1", resources)
			}(i)
		}

		wg.Wait()

		expectedCPU := int64(numAgents * 100)
		expectedMemory := int64(numAgents * 256)

		if node.Allocated.CPUCores != expectedCPU {
			t.Errorf("Allocated CPU = %d, want %d", node.Allocated.CPUCores, expectedCPU)
		}
		if node.Allocated.MemoryMB != expectedMemory {
			t.Errorf("Allocated Memory = %d, want %d", node.Allocated.MemoryMB, expectedMemory)
		}
		if node.AgentCount() != numAgents {
			t.Errorf("Agent count = %d, want %d", node.AgentCount(), numAgents)
		}
	})
}

func TestNodeCanFit(t *testing.T) {
	tests := []struct {
		name      string
		capacity  scheduler.Resources
		allocated scheduler.Resources
		request   scheduler.Resources
		canFit    bool
	}{
		{
			name:      "plenty_of_space",
			capacity:  scheduler.Resources{CPUCores: 4000, MemoryMB: 8192, DiskMB: 102400},
			allocated: scheduler.Resources{CPUCores: 1000, MemoryMB: 2048, DiskMB: 10240},
			request:   scheduler.Resources{CPUCores: 1000, MemoryMB: 1024, DiskMB: 10240},
			canFit:    true,
		},
		{
			name:      "exactly_fits",
			capacity:  scheduler.Resources{CPUCores: 4000, MemoryMB: 8192, DiskMB: 102400},
			allocated: scheduler.Resources{CPUCores: 3000, MemoryMB: 7168, DiskMB: 92160},
			request:   scheduler.Resources{CPUCores: 1000, MemoryMB: 1024, DiskMB: 10240},
			canFit:    true,
		},
		{
			name:      "not_enough_cpu",
			capacity:  scheduler.Resources{CPUCores: 4000, MemoryMB: 8192, DiskMB: 102400},
			allocated: scheduler.Resources{CPUCores: 3500, MemoryMB: 2048, DiskMB: 10240},
			request:   scheduler.Resources{CPUCores: 1000, MemoryMB: 1024, DiskMB: 10240},
			canFit:    false,
		},
		{
			name:      "not_enough_memory",
			capacity:  scheduler.Resources{CPUCores: 4000, MemoryMB: 8192, DiskMB: 102400},
			allocated: scheduler.Resources{CPUCores: 1000, MemoryMB: 7680, DiskMB: 10240},
			request:   scheduler.Resources{CPUCores: 1000, MemoryMB: 1024, DiskMB: 10240},
			canFit:    false,
		},
		{
			name:      "not_enough_disk",
			capacity:  scheduler.Resources{CPUCores: 4000, MemoryMB: 8192, DiskMB: 102400},
			allocated: scheduler.Resources{CPUCores: 1000, MemoryMB: 2048, DiskMB: 100000},
			request:   scheduler.Resources{CPUCores: 1000, MemoryMB: 1024, DiskMB: 10240},
			canFit:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node := &scheduler.Node{
				ID:        "node-1",
				Capacity:  tt.capacity,
				Allocated: tt.allocated,
				Agents:    make(map[api.AgentID]*scheduler.AgentAllocation),
			}

			if got := node.CanFit(tt.request); got != tt.canFit {
				t.Errorf("CanFit() = %v, want %v", got, tt.canFit)
			}
		})
	}
}

// ============================================================================
// Scheduler Lifecycle Tests
// ============================================================================

func TestSchedulerStartStop(t *testing.T) {
	t.Run("start_and_stop", func(t *testing.T) {
		logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
		s := scheduler.New(logger, scheduler.Config{
			Strategy: scheduler.BinPacking,
			Interval: 100 * time.Millisecond,
		})

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		go s.Start(ctx)

		// Give it time to start
		time.Sleep(200 * time.Millisecond)

		// Stop scheduler
		s.Stop()

		// Should stop within reasonable time
		time.Sleep(200 * time.Millisecond)
	})

	t.Run("context_cancellation", func(t *testing.T) {
		logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
		s := scheduler.New(logger, scheduler.Config{
			Strategy: scheduler.BinPacking,
			Interval: 100 * time.Millisecond,
		})

		ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
		defer cancel()

		done := make(chan struct{})
		go func() {
			s.Start(ctx)
			close(done)
		}()

		// Should stop when context is canceled
		select {
		case <-done:
			// Success
		case <-time.After(1 * time.Second):
			t.Fatal("Scheduler did not stop after context cancellation")
		}
	})
}

func TestSchedulerEvents(t *testing.T) {
	t.Run("event_channel_overflow", func(t *testing.T) {
		logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
		s := scheduler.New(logger, scheduler.Config{
			Strategy:        scheduler.BinPacking,
			Interval:        10 * time.Millisecond,
			EventBufferSize: 5, // Small buffer
		})

		ctx := context.Background()

		// Register a node
		node := &scheduler.Node{
			ID:       "node-1",
			Capacity: scheduler.Resources{CPUCores: 4000, MemoryMB: 8192, DiskMB: 102400},
			Agents:   make(map[api.AgentID]*scheduler.AgentAllocation),
		}
		s.RegisterNode(ctx, node)

		// Schedule many agents
		for i := 0; i < 20; i++ {
			req := &scheduler.AgentRequest{
				Config: api.AgentConfig{
					ID:       api.AgentID(fmt.Sprintf("agent-%d", i)),
					TenantID: api.TenantID("tenant-1"),
				},
				Resources: scheduler.Resources{CPUCores: 100, MemoryMB: 128},
				Priority:  10,
				CreatedAt: time.Now(),
			}
			s.ScheduleAgent(ctx, req)
		}

		schedCtx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go s.Start(schedCtx)

		// Some events may be dropped due to buffer overflow, but shouldn't crash
		eventCount := 0
		timeout := time.After(1 * time.Second)
	loop:
		for {
			select {
			case <-s.Events():
				eventCount++
			case <-timeout:
				break loop
			}
		}

		// Should have received some events (but not necessarily all)
		if eventCount == 0 {
			t.Error("Expected to receive some events, got 0")
		}
	})
}

// ============================================================================
// Edge Case Tests
// ============================================================================

func TestSchedulerEdgeCases(t *testing.T) {
	t.Run("schedule_with_no_nodes", func(t *testing.T) {
		logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
		s := scheduler.New(logger, scheduler.Config{
			Strategy: scheduler.BinPacking,
			Interval: 100 * time.Millisecond,
		})

		ctx := context.Background()

		req := &scheduler.AgentRequest{
			Config:    api.AgentConfig{ID: "agent-1", TenantID: "tenant-1"},
			Resources: scheduler.Resources{CPUCores: 1000, MemoryMB: 1024},
			Priority:  10,
			CreatedAt: time.Now(),
		}

		err := s.ScheduleAgent(ctx, req)
		if err != nil {
			t.Fatalf("ScheduleAgent failed: %v", err)
		}

		// Agent should stay in queue
		if s.QueueLength() != 1 {
			t.Errorf("Queue length = %d, want 1", s.QueueLength())
		}
	})

	t.Run("unregister_node_with_agents", func(t *testing.T) {
		logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
		s := scheduler.New(logger, scheduler.Config{Strategy: scheduler.BinPacking})

		ctx := context.Background()

		node := &scheduler.Node{
			ID:       "node-1",
			Capacity: scheduler.Resources{CPUCores: 4000, MemoryMB: 8192, DiskMB: 102400},
			Agents: map[api.AgentID]*scheduler.AgentAllocation{
				"agent-1": {
					AgentID:   "agent-1",
					TenantID:  "tenant-1",
					Resources: scheduler.Resources{CPUCores: 1000, MemoryMB: 1024},
				},
			},
		}

		s.RegisterNode(ctx, node)

		// Should fail to unregister with active agents
		err := s.UnregisterNode(ctx, "node-1")
		if err == nil {
			t.Error("Expected error when unregistering node with active agents, got nil")
		}
	})

	t.Run("unregister_nonexistent_node", func(t *testing.T) {
		logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
		s := scheduler.New(logger, scheduler.Config{Strategy: scheduler.BinPacking})

		ctx := context.Background()

		err := s.UnregisterNode(ctx, "nonexistent")
		if err == nil {
			t.Error("Expected error when unregistering nonexistent node, got nil")
		}
	})

	t.Run("empty_queue_peek_dequeue", func(t *testing.T) {
		queue := scheduler.NewQueue()

		if queue.Peek() != nil {
			t.Error("Peek on empty queue should return nil")
		}

		if queue.Dequeue() != nil {
			t.Error("Dequeue on empty queue should return nil")
		}

		if queue.Len() != 0 {
			t.Errorf("Empty queue length = %d, want 0", queue.Len())
		}
	})
}

// ============================================================================
// Statistics Tests
// ============================================================================

func TestSchedulerStats(t *testing.T) {
	t.Run("accurate_statistics", func(t *testing.T) {
		logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
		s := scheduler.New(logger, scheduler.Config{Strategy: scheduler.BinPacking})

		ctx := context.Background()

		// Register two nodes
		node1 := &scheduler.Node{
			ID:        "node-1",
			Capacity:  scheduler.Resources{CPUCores: 4000, MemoryMB: 8192, DiskMB: 102400},
			Allocated: scheduler.Resources{CPUCores: 1000, MemoryMB: 2048, DiskMB: 10240},
			Agents: map[api.AgentID]*scheduler.AgentAllocation{
				"agent-1": {
					AgentID:   "agent-1",
					TenantID:  "tenant-1",
					Resources: scheduler.Resources{CPUCores: 1000, MemoryMB: 2048},
				},
			},
		}

		node2 := &scheduler.Node{
			ID:        "node-2",
			Capacity:  scheduler.Resources{CPUCores: 4000, MemoryMB: 8192, DiskMB: 102400},
			Allocated: scheduler.Resources{CPUCores: 2000, MemoryMB: 4096, DiskMB: 20480},
			Agents: map[api.AgentID]*scheduler.AgentAllocation{
				"agent-2": {
					AgentID:   "agent-2",
					TenantID:  "tenant-1",
					Resources: scheduler.Resources{CPUCores: 2000, MemoryMB: 4096},
				},
			},
		}

		s.RegisterNode(ctx, node1)
		s.RegisterNode(ctx, node2)

		// Add one queued request
		req := &scheduler.AgentRequest{
			Config:    api.AgentConfig{ID: "agent-3", TenantID: "tenant-1"},
			Resources: scheduler.Resources{CPUCores: 1000, MemoryMB: 1024},
			Priority:  10,
			CreatedAt: time.Now(),
		}
		s.ScheduleAgent(ctx, req)

		stats := s.GetStats()

		if stats.NodeCount != 2 {
			t.Errorf("NodeCount = %d, want 2", stats.NodeCount)
		}
		if stats.AgentCount != 2 {
			t.Errorf("AgentCount = %d, want 2", stats.AgentCount)
		}
		if stats.QueuedRequests != 1 {
			t.Errorf("QueuedRequests = %d, want 1", stats.QueuedRequests)
		}
		if stats.TotalCapacity.CPUCores != 8000 {
			t.Errorf("TotalCapacity.CPUCores = %d, want 8000", stats.TotalCapacity.CPUCores)
		}
		if stats.TotalAllocated.CPUCores != 3000 {
			t.Errorf("TotalAllocated.CPUCores = %d, want 3000", stats.TotalAllocated.CPUCores)
		}

		utilization := stats.UtilizationPercent()
		expectedUtilization := (3000.0 / 8000.0) * 100 // 37.5%
		if utilization != expectedUtilization {
			t.Errorf("UtilizationPercent = %.2f, want %.2f", utilization, expectedUtilization)
		}
	})
}

// ============================================================================
// Additional Coverage Tests
// ============================================================================

func TestSchedulerUnscheduleAgent(t *testing.T) {
	t.Run("unschedule_from_queue", func(t *testing.T) {
		logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
		s := scheduler.New(logger, scheduler.Config{Strategy: scheduler.BinPacking})

		ctx := context.Background()

		// Schedule an agent (no nodes registered, so it stays in queue)
		req := &scheduler.AgentRequest{
			Config:    api.AgentConfig{ID: "agent-1", TenantID: "tenant-1"},
			Resources: scheduler.Resources{CPUCores: 1000, MemoryMB: 1024},
			Priority:  10,
			CreatedAt: time.Now(),
		}
		s.ScheduleAgent(ctx, req)

		if s.QueueLength() != 1 {
			t.Errorf("Queue length = %d, want 1", s.QueueLength())
		}

		// Unschedule the agent
		s.UnscheduleAgent(ctx, "agent-1")

		if s.QueueLength() != 0 {
			t.Errorf("Queue length after unschedule = %d, want 0", s.QueueLength())
		}
	})

	t.Run("unschedule_allocated_agent", func(t *testing.T) {
		logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
		s := scheduler.New(logger, scheduler.Config{
			Strategy: scheduler.BinPacking,
			Interval: 50 * time.Millisecond,
		})

		ctx := context.Background()

		// Register a node
		node := &scheduler.Node{
			ID:       "node-1",
			Capacity: scheduler.Resources{CPUCores: 4000, MemoryMB: 8192, DiskMB: 102400},
			Agents:   make(map[api.AgentID]*scheduler.AgentAllocation),
		}
		s.RegisterNode(ctx, node)

		// Schedule an agent
		req := &scheduler.AgentRequest{
			Config:    api.AgentConfig{ID: "agent-1", TenantID: "tenant-1"},
			Resources: scheduler.Resources{CPUCores: 1000, MemoryMB: 1024},
			Priority:  10,
			CreatedAt: time.Now(),
		}
		s.ScheduleAgent(ctx, req)

		// Start scheduler to allocate the agent
		schedCtx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go s.Start(schedCtx)

		// Wait for allocation
		select {
		case event := <-s.Events():
			if !event.Success {
				t.Fatalf("Scheduling failed: %v", event.Error)
			}
		case <-time.After(500 * time.Millisecond):
			t.Fatal("Timeout waiting for scheduling")
		}

		// Verify agent is allocated
		if node.AgentCount() != 1 {
			t.Errorf("Node agent count = %d, want 1", node.AgentCount())
		}

		// Unschedule the agent (should deallocate from node)
		s.UnscheduleAgent(ctx, "agent-1")

		if node.AgentCount() != 0 {
			t.Errorf("Node agent count after unschedule = %d, want 0", node.AgentCount())
		}
	})
}

func TestSchedulerGetNode(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	s := scheduler.New(logger, scheduler.Config{Strategy: scheduler.BinPacking})

	ctx := context.Background()

	node := &scheduler.Node{
		ID:       "node-1",
		Capacity: scheduler.Resources{CPUCores: 4000, MemoryMB: 8192, DiskMB: 102400},
		Agents:   make(map[api.AgentID]*scheduler.AgentAllocation),
	}
	s.RegisterNode(ctx, node)

	// Test GetNode
	retrievedNode, exists := s.GetNode("node-1")
	if !exists {
		t.Error("GetNode returned false for existing node")
	}
	if retrievedNode.ID != "node-1" {
		t.Errorf("Retrieved node ID = %s, want node-1", retrievedNode.ID)
	}

	// Test GetNode for nonexistent node
	_, exists = s.GetNode("nonexistent")
	if exists {
		t.Error("GetNode returned true for nonexistent node")
	}
}

func TestSchedulerListNodes(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	s := scheduler.New(logger, scheduler.Config{Strategy: scheduler.BinPacking})

	ctx := context.Background()

	// Initially empty
	nodes := s.ListNodes()
	if len(nodes) != 0 {
		t.Errorf("Initial ListNodes length = %d, want 0", len(nodes))
	}

	// Register nodes
	for i := 0; i < 3; i++ {
		node := &scheduler.Node{
			ID:       fmt.Sprintf("node-%d", i),
			Capacity: scheduler.Resources{CPUCores: 4000, MemoryMB: 8192, DiskMB: 102400},
			Agents:   make(map[api.AgentID]*scheduler.AgentAllocation),
		}
		s.RegisterNode(ctx, node)
	}

	nodes = s.ListNodes()
	if len(nodes) != 3 {
		t.Errorf("ListNodes length = %d, want 3", len(nodes))
	}
}

func TestNodeHelperMethods(t *testing.T) {
	t.Run("has_agent", func(t *testing.T) {
		node := &scheduler.Node{
			ID:       "node-1",
			Capacity: scheduler.Resources{CPUCores: 4000, MemoryMB: 8192, DiskMB: 102400},
			Agents:   make(map[api.AgentID]*scheduler.AgentAllocation),
		}

		node.Allocate("agent-1", "tenant-1", scheduler.Resources{CPUCores: 1000, MemoryMB: 1024})

		if !node.HasAgent("agent-1") {
			t.Error("HasAgent returned false for allocated agent")
		}

		if node.HasAgent("nonexistent") {
			t.Error("HasAgent returned true for nonexistent agent")
		}
	})

	t.Run("agent_ids", func(t *testing.T) {
		node := &scheduler.Node{
			ID:       "node-1",
			Capacity: scheduler.Resources{CPUCores: 4000, MemoryMB: 8192, DiskMB: 102400},
			Agents:   make(map[api.AgentID]*scheduler.AgentAllocation),
		}

		// Empty node
		ids := node.AgentIDs()
		if len(ids) != 0 {
			t.Errorf("AgentIDs length = %d, want 0", len(ids))
		}

		// Add agents
		node.Allocate("agent-1", "tenant-1", scheduler.Resources{CPUCores: 1000, MemoryMB: 1024})
		node.Allocate("agent-2", "tenant-1", scheduler.Resources{CPUCores: 1000, MemoryMB: 1024})

		ids = node.AgentIDs()
		if len(ids) != 2 {
			t.Errorf("AgentIDs length = %d, want 2", len(ids))
		}

		// Verify IDs are present
		idMap := make(map[string]bool)
		for _, id := range ids {
			idMap[id] = true
		}
		if !idMap["agent-1"] || !idMap["agent-2"] {
			t.Error("AgentIDs did not return expected agent IDs")
		}
	})

	t.Run("deallocate_nonexistent_agent", func(t *testing.T) {
		node := &scheduler.Node{
			ID:       "node-1",
			Capacity: scheduler.Resources{CPUCores: 4000, MemoryMB: 8192, DiskMB: 102400},
			Agents:   make(map[api.AgentID]*scheduler.AgentAllocation),
		}

		// Deallocating nonexistent agent should not panic
		node.Deallocate("nonexistent")

		if node.AgentCount() != 0 {
			t.Errorf("AgentCount = %d, want 0", node.AgentCount())
		}
	})

	t.Run("utilization_zero_capacity", func(t *testing.T) {
		node := &scheduler.Node{
			ID:       "node-1",
			Capacity: scheduler.Resources{CPUCores: 0, MemoryMB: 0, DiskMB: 0},
			Agents:   make(map[api.AgentID]*scheduler.AgentAllocation),
		}

		utilization := node.UtilizationPercent()
		if utilization != 0.0 {
			t.Errorf("UtilizationPercent with zero capacity = %.2f, want 0.0", utilization)
		}
	})
}

func TestSchedulerStatsEdgeCases(t *testing.T) {
	t.Run("stats_zero_capacity", func(t *testing.T) {
		logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
		s := scheduler.New(logger, scheduler.Config{Strategy: scheduler.BinPacking})

		ctx := context.Background()

		// Register node with zero capacity
		node := &scheduler.Node{
			ID:       "node-1",
			Capacity: scheduler.Resources{CPUCores: 0, MemoryMB: 0, DiskMB: 0},
			Agents:   make(map[api.AgentID]*scheduler.AgentAllocation),
		}
		s.RegisterNode(ctx, node)

		stats := s.GetStats()
		utilization := stats.UtilizationPercent()
		if utilization != 0.0 {
			t.Errorf("UtilizationPercent with zero capacity = %.2f, want 0.0", utilization)
		}
	})
}

// ============================================================================
// Integration Tests
// ============================================================================

func TestSchedulerIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	t.Run("full_workflow_multiple_agents", func(t *testing.T) {
		logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
		s := scheduler.New(logger, scheduler.Config{
			Strategy:        scheduler.BinPacking,
			Interval:        50 * time.Millisecond,
			EventBufferSize: 100,
		})

		ctx := context.Background()

		// Register nodes
		for i := 0; i < 3; i++ {
			node := &scheduler.Node{
				ID:       fmt.Sprintf("node-%d", i),
				Capacity: scheduler.Resources{CPUCores: 4000, MemoryMB: 8192, DiskMB: 102400},
				Agents:   make(map[api.AgentID]*scheduler.AgentAllocation),
			}
			s.RegisterNode(ctx, node)
		}

		// Start scheduler
		schedCtx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go s.Start(schedCtx)

		// Schedule multiple agents
		const numAgents = 10
		for i := 0; i < numAgents; i++ {
			req := &scheduler.AgentRequest{
				Config: api.AgentConfig{
					ID:       api.AgentID(fmt.Sprintf("agent-%d", i)),
					TenantID: api.TenantID("tenant-1"),
				},
				Resources: scheduler.Resources{CPUCores: 500, MemoryMB: 512},
				Priority:  i,
				CreatedAt: time.Now(),
			}
			s.ScheduleAgent(ctx, req)
		}

		// Collect events
		successfulSchedules := 0
		timeout := time.After(2 * time.Second)
	loop:
		for successfulSchedules < numAgents {
			select {
			case event := <-s.Events():
				if event.Success {
					successfulSchedules++
				}
			case <-timeout:
				break loop
			}
		}

		if successfulSchedules != numAgents {
			t.Errorf("Scheduled agents = %d, want %d", successfulSchedules, numAgents)
		}

		// Verify stats
		stats := s.GetStats()
		if stats.AgentCount != numAgents {
			t.Errorf("AgentCount = %d, want %d", stats.AgentCount, numAgents)
		}
		if stats.QueuedRequests != 0 {
			t.Errorf("QueuedRequests = %d, want 0", stats.QueuedRequests)
		}
	})
}
