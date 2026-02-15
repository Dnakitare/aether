package scheduler_test

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/aether-runtime/aether/internal/scheduler"
	"github.com/aether-runtime/aether/pkg/api"
)

func TestScheduler(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelError, // Reduce noise in tests
	}))

	config := scheduler.Config{
		Strategy:        scheduler.BinPacking,
		Interval:        100 * time.Millisecond,
		EventBufferSize: 10,
	}

	s := scheduler.New(logger, config)

	ctx := context.Background()

	// Register a node
	node := &scheduler.Node{
		ID:   "node-1",
		Name: "Test Node",
		Capacity: scheduler.Resources{
			CPUCores: 4000, // 4 cores
			MemoryMB: 8192, // 8GB
			DiskMB:   102400,
		},
		Allocated: scheduler.Resources{},
		Agents:    make(map[api.AgentID]*scheduler.AgentAllocation),
	}

	s.RegisterNode(ctx, node)

	// Create an agent request
	agentConfig := api.AgentConfig{
		ID:       api.AgentID("agent-1"),
		TenantID: api.TenantID("tenant-1"),
		Name:     "Test Agent",
		Image:    "python:3.11",
		Resources: api.ResourceLimits{
			CPUCount: 2,
			MemoryMB: 1024,
		},
	}

	req := &scheduler.AgentRequest{
		Config:    agentConfig,
		Resources: scheduler.FromAgentConfig(agentConfig),
		Priority:  10,
		CreatedAt: time.Now(),
	}

	// Schedule the agent
	err := s.ScheduleAgent(ctx, req)
	if err != nil {
		t.Fatalf("Failed to schedule agent: %v", err)
	}

	// Start scheduler
	schedCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go s.Start(schedCtx)

	// Wait for scheduling event
	select {
	case event := <-s.Events():
		if !event.Success {
			t.Fatalf("Scheduling failed: %v", event.Error)
		}
		if event.AgentID != agentConfig.ID {
			t.Errorf("Event agent ID = %s, want %s", event.AgentID, agentConfig.ID)
		}
		if event.NodeID != node.ID {
			t.Errorf("Event node ID = %s, want %s", event.NodeID, node.ID)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Timeout waiting for scheduling event")
	}

	// Verify stats
	stats := s.GetStats()
	if stats.AgentCount != 1 {
		t.Errorf("AgentCount = %d, want 1", stats.AgentCount)
	}
	if stats.QueuedRequests != 0 {
		t.Errorf("QueuedRequests = %d, want 0", stats.QueuedRequests)
	}
}

func TestSchedulerNoCapacity(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	config := scheduler.Config{
		Strategy:        scheduler.BinPacking,
		Interval:        100 * time.Millisecond,
		EventBufferSize: 10,
	}

	s := scheduler.New(logger, config)

	ctx := context.Background()

	// Register a small node
	node := &scheduler.Node{
		ID:   "node-1",
		Name: "Small Node",
		Capacity: scheduler.Resources{
			CPUCores: 1000, // 1 core
			MemoryMB: 512,  // 512MB
			DiskMB:   10240,
		},
		Allocated: scheduler.Resources{},
		Agents:    make(map[api.AgentID]*scheduler.AgentAllocation),
	}

	s.RegisterNode(ctx, node)

	// Request more than available
	agentConfig := api.AgentConfig{
		ID:       api.AgentID("agent-big"),
		TenantID: api.TenantID("tenant-1"),
		Name:     "Big Agent",
		Image:    "python:3.11",
		Resources: api.ResourceLimits{
			CPUCount: 4,    // Need 4 cores
			MemoryMB: 8192, // Need 8GB
		},
	}

	req := &scheduler.AgentRequest{
		Config:    agentConfig,
		Resources: scheduler.FromAgentConfig(agentConfig),
		Priority:  10,
		CreatedAt: time.Now(),
	}

	err := s.ScheduleAgent(ctx, req)
	if err != nil {
		t.Fatalf("Failed to queue agent: %v", err)
	}

	// Start scheduler
	schedCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go s.Start(schedCtx)

	// Should fail to schedule
	select {
	case event := <-s.Events():
		if event.Success {
			t.Fatal("Expected scheduling to fail but it succeeded")
		}
	case <-time.After(500 * time.Millisecond):
		// Expected - agent stays in queue
	}

	// Verify agent is still queued
	if s.QueueLength() != 1 {
		t.Errorf("QueueLength = %d, want 1", s.QueueLength())
	}
}

func TestPlacementStrategies(t *testing.T) {
	strategies := []scheduler.PlacementStrategy{
		scheduler.BinPacking,
		scheduler.Spread,
		scheduler.BestFit,
	}

	for _, strategy := range strategies {
		t.Run(string(strategy), func(t *testing.T) {
			logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
				Level: slog.LevelError,
			}))

			config := scheduler.Config{
				Strategy:        strategy,
				Interval:        100 * time.Millisecond,
				EventBufferSize: 10,
			}

			s := scheduler.New(logger, config)

			ctx := context.Background()

			// Register two nodes
			node1 := &scheduler.Node{
				ID:   "node-1",
				Name: "Node 1",
				Capacity: scheduler.Resources{
					CPUCores: 4000,
					MemoryMB: 8192,
					DiskMB:   102400,
				},
				Allocated: scheduler.Resources{},
				Agents:    make(map[api.AgentID]*scheduler.AgentAllocation),
			}

			node2 := &scheduler.Node{
				ID:   "node-2",
				Name: "Node 2",
				Capacity: scheduler.Resources{
					CPUCores: 4000,
					MemoryMB: 8192,
					DiskMB:   102400,
				},
				Allocated: scheduler.Resources{},
				Agents:    make(map[api.AgentID]*scheduler.AgentAllocation),
			}

			s.RegisterNode(ctx, node1)
			s.RegisterNode(ctx, node2)

			// Schedule an agent
			agentConfig := api.AgentConfig{
				ID:       api.AgentID("agent-1"),
				TenantID: api.TenantID("tenant-1"),
				Name:     "Test Agent",
				Image:    "python:3.11",
				Resources: api.ResourceLimits{
					CPUCount: 1,
					MemoryMB: 512,
				},
			}

			req := &scheduler.AgentRequest{
				Config:    agentConfig,
				Resources: scheduler.FromAgentConfig(agentConfig),
				Priority:  10,
				CreatedAt: time.Now(),
			}

			err := s.ScheduleAgent(ctx, req)
			if err != nil {
				t.Fatalf("Failed to schedule agent: %v", err)
			}

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			go s.Start(ctx)

			// Wait for scheduling
			select {
			case event := <-s.Events():
				if !event.Success {
					t.Fatalf("Scheduling failed: %v", event.Error)
				}
			case <-time.After(1 * time.Second):
				t.Fatal("Timeout waiting for scheduling event")
			}
		})
	}
}
