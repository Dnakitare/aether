package distributed

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/aether-runtime/aether/internal/scheduler"
	"github.com/aether-runtime/aether/pkg/api"
)

// Skip these tests if Redis is not available
func skipIfNoRedis(t *testing.T) {
	if os.Getenv("REDIS_AVAILABLE") != "true" {
		t.Skip("Skipping test: Redis not available (set REDIS_AVAILABLE=true to run)")
	}
}

func TestNodeRegistry_RegisterAndGet(t *testing.T) {
	skipIfNoRedis(t)

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	config := DefaultNodeRegistryConfig()
	config.KeyPrefix = "aether:test"

	nr, err := NewNodeRegistry(logger, config)
	if err != nil {
		t.Fatalf("Failed to create node registry: %v", err)
	}
	defer nr.Close()

	ctx := context.Background()

	// Create test node
	node := &scheduler.Node{
		ID:   "node-001",
		Name: "Test Node",
		Labels: map[string]string{
			"region": "us-west-2",
			"zone":   "us-west-2a",
		},
		Capacity: scheduler.Resources{
			CPUCores: 8000,
			MemoryMB: 16384,
			DiskMB:   102400,
		},
		Allocated: scheduler.Resources{
			CPUCores: 2000,
			MemoryMB: 4096,
			DiskMB:   20480,
		},
		Agents: make(map[api.AgentID]*scheduler.AgentAllocation),
	}

	// Register node
	if err := nr.RegisterNode(ctx, node, "scheduler-1"); err != nil {
		t.Fatalf("Failed to register node: %v", err)
	}

	// Get node
	retrievedNode, schedulerID, err := nr.GetNode(ctx, "node-001")
	if err != nil {
		t.Fatalf("Failed to get node: %v", err)
	}

	// Verify node data
	if retrievedNode.ID != node.ID {
		t.Errorf("Node ID = %s, want %s", retrievedNode.ID, node.ID)
	}

	if retrievedNode.Name != node.Name {
		t.Errorf("Node Name = %s, want %s", retrievedNode.Name, node.Name)
	}

	if schedulerID != "scheduler-1" {
		t.Errorf("Scheduler ID = %s, want scheduler-1", schedulerID)
	}

	if retrievedNode.Capacity.CPUCores != 8000 {
		t.Errorf("Capacity CPU = %d, want 8000", retrievedNode.Capacity.CPUCores)
	}

	if retrievedNode.Allocated.MemoryMB != 4096 {
		t.Errorf("Allocated Memory = %d, want 4096", retrievedNode.Allocated.MemoryMB)
	}

	// Cleanup
	nr.UnregisterNode(ctx, "node-001", "scheduler-1")
}

func TestNodeRegistry_GetOwnedNodes(t *testing.T) {
	skipIfNoRedis(t)

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	config := DefaultNodeRegistryConfig()
	config.KeyPrefix = "aether:test"

	nr, err := NewNodeRegistry(logger, config)
	if err != nil {
		t.Fatalf("Failed to create node registry: %v", err)
	}
	defer nr.Close()

	ctx := context.Background()

	// Register nodes for different schedulers
	for i := 1; i <= 3; i++ {
		node := &scheduler.Node{
			ID:        fmt.Sprintf("node-%03d", i),
			Name:      fmt.Sprintf("Node %d", i),
			Labels:    map[string]string{},
			Capacity:  scheduler.Resources{CPUCores: 4000, MemoryMB: 8192, DiskMB: 51200},
			Allocated: scheduler.Resources{},
			Agents:    make(map[api.AgentID]*scheduler.AgentAllocation),
		}

		schedulerID := "scheduler-1"
		if i > 2 {
			schedulerID = "scheduler-2"
		}

		if err := nr.RegisterNode(ctx, node, schedulerID); err != nil {
			t.Fatalf("Failed to register node %d: %v", i, err)
		}
	}

	// Get nodes owned by scheduler-1
	nodes, err := nr.GetOwnedNodes(ctx, "scheduler-1")
	if err != nil {
		t.Fatalf("Failed to get owned nodes: %v", err)
	}

	if len(nodes) != 2 {
		t.Errorf("Owned nodes count = %d, want 2", len(nodes))
	}

	// Get nodes owned by scheduler-2
	nodes, err = nr.GetOwnedNodes(ctx, "scheduler-2")
	if err != nil {
		t.Fatalf("Failed to get owned nodes for scheduler-2: %v", err)
	}

	if len(nodes) != 1 {
		t.Errorf("Owned nodes count for scheduler-2 = %d, want 1", len(nodes))
	}

	// Cleanup
	for i := 1; i <= 3; i++ {
		schedulerID := "scheduler-1"
		if i > 2 {
			schedulerID = "scheduler-2"
		}
		nr.UnregisterNode(ctx, fmt.Sprintf("node-%03d", i), schedulerID)
	}
}

func TestNodeRegistry_TryAllocate(t *testing.T) {
	skipIfNoRedis(t)

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	config := DefaultNodeRegistryConfig()
	config.KeyPrefix = "aether:test"

	nr, err := NewNodeRegistry(logger, config)
	if err != nil {
		t.Fatalf("Failed to create node registry: %v", err)
	}
	defer nr.Close()

	ctx := context.Background()

	// Create test node
	node := &scheduler.Node{
		ID:     "node-001",
		Name:   "Test Node",
		Labels: map[string]string{},
		Capacity: scheduler.Resources{
			CPUCores: 4000,
			MemoryMB: 8192,
			DiskMB:   51200,
		},
		Allocated: scheduler.Resources{},
		Agents:    make(map[api.AgentID]*scheduler.AgentAllocation),
	}

	if err := nr.RegisterNode(ctx, node, "scheduler-1"); err != nil {
		t.Fatalf("Failed to register node: %v", err)
	}

	// Allocate resources (should succeed)
	resources := scheduler.Resources{
		CPUCores: 2000,
		MemoryMB: 4096,
		DiskMB:   10240,
	}

	success, err := nr.TryAllocate(ctx, "node-001", "agent-001", resources)
	if err != nil {
		t.Fatalf("TryAllocate failed: %v", err)
	}

	if !success {
		t.Error("TryAllocate returned false, expected true")
	}

	// Verify allocation
	updatedNode, _, err := nr.GetNode(ctx, "node-001")
	if err != nil {
		t.Fatalf("Failed to get node after allocation: %v", err)
	}

	if updatedNode.Allocated.CPUCores != 2000 {
		t.Errorf("Allocated CPU = %d, want 2000", updatedNode.Allocated.CPUCores)
	}

	if len(updatedNode.Agents) != 1 {
		t.Errorf("Agent count = %d, want 1", len(updatedNode.Agents))
	}

	// Try to allocate more than available (should fail)
	largeResources := scheduler.Resources{
		CPUCores: 3000, // Only 2000 remaining
		MemoryMB: 4096,
		DiskMB:   10240,
	}

	success, err = nr.TryAllocate(ctx, "node-001", "agent-002", largeResources)
	if err != nil {
		t.Fatalf("TryAllocate failed: %v", err)
	}

	if success {
		t.Error("TryAllocate returned true, expected false (insufficient resources)")
	}

	// Cleanup
	nr.UnregisterNode(ctx, "node-001", "scheduler-1")
}

func TestNodeRegistry_Deallocate(t *testing.T) {
	skipIfNoRedis(t)

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	config := DefaultNodeRegistryConfig()
	config.KeyPrefix = "aether:test"

	nr, err := NewNodeRegistry(logger, config)
	if err != nil {
		t.Fatalf("Failed to create node registry: %v", err)
	}
	defer nr.Close()

	ctx := context.Background()

	// Create and register node
	node := &scheduler.Node{
		ID:     "node-001",
		Name:   "Test Node",
		Labels: map[string]string{},
		Capacity: scheduler.Resources{
			CPUCores: 4000,
			MemoryMB: 8192,
			DiskMB:   51200,
		},
		Allocated: scheduler.Resources{},
		Agents:    make(map[api.AgentID]*scheduler.AgentAllocation),
	}

	if err := nr.RegisterNode(ctx, node, "scheduler-1"); err != nil {
		t.Fatalf("Failed to register node: %v", err)
	}

	// Allocate resources
	resources := scheduler.Resources{
		CPUCores: 2000,
		MemoryMB: 4096,
		DiskMB:   10240,
	}

	success, err := nr.TryAllocate(ctx, "node-001", "agent-001", resources)
	if err != nil || !success {
		t.Fatalf("TryAllocate failed: success=%v, err=%v", success, err)
	}

	// Deallocate resources
	if err := nr.Deallocate(ctx, "node-001", "agent-001", resources); err != nil {
		t.Fatalf("Deallocate failed: %v", err)
	}

	// Verify deallocation
	updatedNode, _, err := nr.GetNode(ctx, "node-001")
	if err != nil {
		t.Fatalf("Failed to get node after deallocation: %v", err)
	}

	if updatedNode.Allocated.CPUCores != 0 {
		t.Errorf("Allocated CPU after deallocation = %d, want 0", updatedNode.Allocated.CPUCores)
	}

	if len(updatedNode.Agents) != 0 {
		t.Errorf("Agent count after deallocation = %d, want 0", len(updatedNode.Agents))
	}

	// Cleanup
	nr.UnregisterNode(ctx, "node-001", "scheduler-1")
}

func TestNodeRegistry_TTL(t *testing.T) {
	skipIfNoRedis(t)

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	config := DefaultNodeRegistryConfig()
	config.KeyPrefix = "aether:test"
	config.NodeTTL = 2 * time.Second // Short TTL for testing

	nr, err := NewNodeRegistry(logger, config)
	if err != nil {
		t.Fatalf("Failed to create node registry: %v", err)
	}
	defer nr.Close()

	ctx := context.Background()

	// Register node
	node := &scheduler.Node{
		ID:        "node-001",
		Name:      "Test Node",
		Labels:    map[string]string{},
		Capacity:  scheduler.Resources{CPUCores: 4000, MemoryMB: 8192, DiskMB: 51200},
		Allocated: scheduler.Resources{},
		Agents:    make(map[api.AgentID]*scheduler.AgentAllocation),
	}

	if err := nr.RegisterNode(ctx, node, "scheduler-1"); err != nil {
		t.Fatalf("Failed to register node: %v", err)
	}

	// Node should exist
	_, _, err = nr.GetNode(ctx, "node-001")
	if err != nil {
		t.Errorf("Node not found immediately after registration: %v", err)
	}

	// Wait for TTL to expire
	time.Sleep(3 * time.Second)

	// Node should be gone
	_, _, err = nr.GetNode(ctx, "node-001")
	if err == nil {
		t.Error("Node still exists after TTL expiry")
	}
}

func TestNodeRegistry_RefreshTTL(t *testing.T) {
	skipIfNoRedis(t)

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	config := DefaultNodeRegistryConfig()
	config.KeyPrefix = "aether:test"
	config.NodeTTL = 3 * time.Second

	nr, err := NewNodeRegistry(logger, config)
	if err != nil {
		t.Fatalf("Failed to create node registry: %v", err)
	}
	defer nr.Close()

	ctx := context.Background()

	// Register node
	node := &scheduler.Node{
		ID:        "node-001",
		Name:      "Test Node",
		Labels:    map[string]string{},
		Capacity:  scheduler.Resources{CPUCores: 4000, MemoryMB: 8192, DiskMB: 51200},
		Allocated: scheduler.Resources{},
		Agents:    make(map[api.AgentID]*scheduler.AgentAllocation),
	}

	if err := nr.RegisterNode(ctx, node, "scheduler-1"); err != nil {
		t.Fatalf("Failed to register node: %v", err)
	}

	// Refresh TTL periodically
	for i := 0; i < 3; i++ {
		time.Sleep(2 * time.Second)
		if err := nr.RefreshTTL(ctx, "node-001"); err != nil {
			t.Fatalf("Failed to refresh TTL: %v", err)
		}
	}

	// Node should still exist (7 seconds total, but TTL refreshed)
	_, _, err = nr.GetNode(ctx, "node-001")
	if err != nil {
		t.Errorf("Node not found after TTL refresh: %v", err)
	}

	// Cleanup
	nr.UnregisterNode(ctx, "node-001", "scheduler-1")
}
