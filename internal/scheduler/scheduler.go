// Package scheduler provides agent scheduling and placement.
package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/dnakitare/aether/pkg/api"
)

// Scheduler manages agent scheduling and placement.
type Scheduler struct {
	logger *slog.Logger
	mu     sync.RWMutex

	// Queue for pending agent requests
	queue *Queue

	// Placer for placement decisions
	placer *Placer

	// Nodes available for scheduling
	nodes map[string]*Node

	// Event channel for scheduling results
	events chan ScheduleEvent

	// Stop channel
	stopCh chan struct{}

	// Scheduling interval
	interval time.Duration
}

// ScheduleEvent represents the result of a scheduling attempt.
type ScheduleEvent struct {
	AgentID   api.AgentID
	NodeID    string
	Success   bool
	Error     error
	Timestamp time.Time
}

// Config holds scheduler configuration.
type Config struct {
	// Strategy for agent placement
	Strategy PlacementStrategy

	// Interval between scheduling cycles
	Interval time.Duration

	// EventBufferSize is the size of the event channel buffer
	EventBufferSize int
}

// New creates a new scheduler.
func New(logger *slog.Logger, config Config) *Scheduler {
	if config.Interval == 0 {
		config.Interval = 1 * time.Second
	}
	if config.EventBufferSize == 0 {
		config.EventBufferSize = 100
	}

	return &Scheduler{
		logger:   logger.With("component", "scheduler"),
		queue:    NewQueue(),
		placer:   NewPlacer(config.Strategy),
		nodes:    make(map[string]*Node),
		events:   make(chan ScheduleEvent, config.EventBufferSize),
		stopCh:   make(chan struct{}),
		interval: config.Interval,
	}
}

// Start begins the scheduling loop.
func (s *Scheduler) Start(ctx context.Context) {
	s.logger.InfoContext(ctx, "starting scheduler", "interval", s.interval)

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			s.logger.InfoContext(ctx, "scheduler stopping due to context cancellation")
			return
		case <-s.stopCh:
			s.logger.InfoContext(ctx, "scheduler stopped")
			return
		case <-ticker.C:
			s.scheduleNext(ctx)
		}
	}
}

// Stop stops the scheduler.
func (s *Scheduler) Stop() {
	close(s.stopCh)
}

// Events returns the event channel for monitoring scheduling results.
func (s *Scheduler) Events() <-chan ScheduleEvent {
	return s.events
}

// ScheduleAgent adds an agent to the scheduling queue.
func (s *Scheduler) ScheduleAgent(req *AgentRequest) error {
	s.logger.InfoContext(context.Background(),
		"queuing agent for scheduling",
		"agent_id", req.Config.ID,
		"tenant_id", req.Config.TenantID,
		"cpu_cores", req.Resources.CPUCores,
		"memory_mb", req.Resources.MemoryMB,
	)

	s.queue.Enqueue(req)
	return nil
}

// UnscheduleAgent removes an agent from the scheduling queue or node.
func (s *Scheduler) UnscheduleAgent(agentID api.AgentID) {
	s.logger.InfoContext(context.Background(), "unscheduling agent", "agent_id", agentID)

	// Remove from queue if pending
	s.queue.Remove(agentID)

	// Remove from node if allocated
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, node := range s.nodes {
		if _, exists := node.Agents[agentID]; exists {
			node.Deallocate(agentID)
			s.logger.InfoContext(context.Background(),
				"deallocated agent from node",
				"agent_id", agentID,
				"node_id", node.ID,
			)
			return
		}
	}
}

// RegisterNode adds a node to the scheduler.
func (s *Scheduler) RegisterNode(node *Node) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.nodes[node.ID] = node
	s.logger.InfoContext(context.Background(),
		"registered node",
		"node_id", node.ID,
		"cpu_cores", node.Capacity.CPUCores,
		"memory_mb", node.Capacity.MemoryMB,
	)
}

// UnregisterNode removes a node from the scheduler.
func (s *Scheduler) UnregisterNode(nodeID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	node, exists := s.nodes[nodeID]
	if !exists {
		return fmt.Errorf("node %s not found", nodeID)
	}

	// Check if node has any agents
	if len(node.Agents) > 0 {
		return fmt.Errorf("cannot unregister node %s: has %d active agents", nodeID, len(node.Agents))
	}

	delete(s.nodes, nodeID)
	s.logger.InfoContext(context.Background(), "unregistered node", "node_id", nodeID)
	return nil
}

// GetNode returns a node by ID.
func (s *Scheduler) GetNode(nodeID string) (*Node, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	node, exists := s.nodes[nodeID]
	return node, exists
}

// ListNodes returns all registered nodes.
func (s *Scheduler) ListNodes() []*Node {
	s.mu.RLock()
	defer s.mu.RUnlock()

	nodes := make([]*Node, 0, len(s.nodes))
	for _, node := range s.nodes {
		nodes = append(nodes, node)
	}
	return nodes
}

// QueueLength returns the number of pending agent requests.
func (s *Scheduler) QueueLength() int {
	return s.queue.Len()
}

// scheduleNext attempts to schedule the next agent in the queue.
func (s *Scheduler) scheduleNext(ctx context.Context) {
	req := s.queue.Peek()
	if req == nil {
		return // Queue is empty
	}

	s.mu.RLock()
	nodeList := make([]*Node, 0, len(s.nodes))
	for _, node := range s.nodes {
		nodeList = append(nodeList, node)
	}
	s.mu.RUnlock()

	// Try to find a suitable node
	node, err := s.placer.SelectNode(req, nodeList)
	if err != nil {
		// No suitable node found - leave in queue and try again later
		s.logger.DebugContext(ctx,
			"no suitable node found for agent",
			"agent_id", req.Config.ID,
			"error", err,
		)

		// Send event about failed scheduling
		s.sendEvent(ScheduleEvent{
			AgentID:   req.Config.ID,
			Success:   false,
			Error:     err,
			Timestamp: time.Now(),
		})
		return
	}

	// Allocate resources on the node
	s.mu.Lock()
	node.Allocate(req.Config.ID, req.Config.TenantID, req.Resources)
	s.mu.Unlock()

	// Remove from queue (successfully scheduled)
	s.queue.Dequeue()

	s.logger.InfoContext(ctx,
		"scheduled agent",
		"agent_id", req.Config.ID,
		"node_id", node.ID,
		"utilization", fmt.Sprintf("%.1f%%", node.UtilizationPercent()),
	)

	// Send event about successful scheduling
	s.sendEvent(ScheduleEvent{
		AgentID:   req.Config.ID,
		NodeID:    node.ID,
		Success:   true,
		Timestamp: time.Now(),
	})
}

// sendEvent sends a scheduling event to the event channel (non-blocking).
func (s *Scheduler) sendEvent(event ScheduleEvent) {
	select {
	case s.events <- event:
	default:
		// Event channel is full, log and drop
		s.logger.WarnContext(context.Background(),
			"event channel full, dropping event",
			"agent_id", event.AgentID,
		)
	}
}

// GetStats returns current scheduler statistics.
func (s *Scheduler) GetStats() SchedulerStats {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var totalCapacity, totalAllocated Resources
	agentCount := 0

	for _, node := range s.nodes {
		totalCapacity.CPUCores += node.Capacity.CPUCores
		totalCapacity.MemoryMB += node.Capacity.MemoryMB
		totalCapacity.DiskMB += node.Capacity.DiskMB

		totalAllocated.CPUCores += node.Allocated.CPUCores
		totalAllocated.MemoryMB += node.Allocated.MemoryMB
		totalAllocated.DiskMB += node.Allocated.DiskMB

		agentCount += len(node.Agents)
	}

	return SchedulerStats{
		NodeCount:      len(s.nodes),
		AgentCount:     agentCount,
		QueuedRequests: s.queue.Len(),
		TotalCapacity:  totalCapacity,
		TotalAllocated: totalAllocated,
	}
}

// SchedulerStats contains scheduler statistics.
type SchedulerStats struct {
	NodeCount      int
	AgentCount     int
	QueuedRequests int
	TotalCapacity  Resources
	TotalAllocated Resources
}

// UtilizationPercent returns overall CPU utilization percentage.
func (s *SchedulerStats) UtilizationPercent() float64 {
	if s.TotalCapacity.CPUCores == 0 {
		return 0
	}
	return float64(s.TotalAllocated.CPUCores) / float64(s.TotalCapacity.CPUCores) * 100
}
