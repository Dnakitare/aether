// Package scheduler provides agent scheduling and placement.
package scheduler

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/aether-runtime/aether/pkg/api"
)

// MetricsRecorder defines the interface for recording scheduler metrics.
type MetricsRecorder interface {
	RecordSchedulingDuration(strategy string, tenantID api.TenantID, duration time.Duration)
	RecordSchedulingError(reason string, tenantID api.TenantID)
}

// Scheduler manages agent scheduling and placement.
type Scheduler struct {
	logger  *slog.Logger
	tracer  trace.Tracer
	metrics MetricsRecorder
	mu      sync.RWMutex

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
		tracer:   otel.Tracer("aether.scheduler"),
		queue:    NewQueue(),
		placer:   NewPlacer(config.Strategy),
		nodes:    make(map[string]*Node),
		events:   make(chan ScheduleEvent, config.EventBufferSize),
		stopCh:   make(chan struct{}),
		interval: config.Interval,
	}
}

// SetMetrics sets the metrics recorder (optional).
func (s *Scheduler) SetMetrics(metrics MetricsRecorder) {
	s.metrics = metrics
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
func (s *Scheduler) ScheduleAgent(ctx context.Context, req *AgentRequest) error {
	ctx, span := s.tracer.Start(ctx, "scheduler.ScheduleAgent",
		trace.WithAttributes(
			attribute.String("agent.id", string(req.Config.ID)),
			attribute.String("agent.tenant_id", string(req.Config.TenantID)),
			attribute.Int64("agent.cpu_cores", req.Resources.CPUCores),
			attribute.Int64("agent.memory_mb", req.Resources.MemoryMB),
		),
	)
	defer span.End()

	s.logger.InfoContext(ctx,
		"queuing agent for scheduling",
		"agent_id", req.Config.ID,
		"tenant_id", req.Config.TenantID,
		"cpu_cores", req.Resources.CPUCores,
		"memory_mb", req.Resources.MemoryMB,
	)

	s.queue.Enqueue(req)
	span.SetAttributes(attribute.Int("queue.length", s.queue.Len()))
	return nil
}

// UnscheduleAgent removes an agent from the scheduling queue or node.
func (s *Scheduler) UnscheduleAgent(ctx context.Context, agentID api.AgentID) {
	s.logger.InfoContext(ctx, "unscheduling agent", "agent_id", agentID)

	// Remove from queue if pending
	s.queue.Remove(agentID)

	// Remove from node if allocated
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, node := range s.nodes {
		if node.HasAgent(agentID) {
			node.Deallocate(agentID)
			s.logger.InfoContext(ctx,
				"deallocated agent from node",
				"agent_id", agentID,
				"node_id", node.ID,
			)
			return
		}
	}
}

// RegisterNode adds a node to the scheduler.
func (s *Scheduler) RegisterNode(ctx context.Context, node *Node) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.nodes[node.ID] = node
	s.logger.InfoContext(ctx,
		"registered node",
		"node_id", node.ID,
		"cpu_cores", node.Capacity.CPUCores,
		"memory_mb", node.Capacity.MemoryMB,
	)
}

// UnregisterNode removes a node from the scheduler.
func (s *Scheduler) UnregisterNode(ctx context.Context, nodeID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	node, exists := s.nodes[nodeID]
	if !exists {
		return fmt.Errorf("node %s not found", nodeID)
	}

	// Check if node has any agents
	agentCount := node.AgentCount()
	if agentCount > 0 {
		return fmt.Errorf("cannot unregister node %s: has %d active agents", nodeID, agentCount)
	}

	delete(s.nodes, nodeID)
	s.logger.InfoContext(ctx, "unregistered node", "node_id", nodeID)
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

	// Track scheduling duration
	startTime := time.Now()

	ctx, span := s.tracer.Start(ctx, "scheduler.scheduleNext",
		trace.WithAttributes(
			attribute.String("agent.id", string(req.Config.ID)),
			attribute.Int("queue.length", s.queue.Len()),
		),
	)
	defer span.End()

	// Lock for the entire check-and-allocate operation to prevent race conditions
	// This ensures no other goroutine can allocate resources between our check and allocation
	s.mu.Lock()
	defer s.mu.Unlock()

	nodeList := make([]*Node, 0, len(s.nodes))
	for _, node := range s.nodes {
		nodeList = append(nodeList, node)
	}
	span.SetAttributes(attribute.Int("nodes.available", len(nodeList)))

	// Try to find a suitable node
	node, err := s.placer.SelectNode(req, nodeList)
	if err != nil {
		// No suitable node found - leave in queue and try again later
		span.SetStatus(codes.Error, "no suitable node found")
		span.RecordError(err)

		// Record scheduling error metric
		if s.metrics != nil {
			s.metrics.RecordSchedulingError("no_suitable_node", req.Config.TenantID)
			s.metrics.RecordSchedulingDuration(string(s.placer.strategy), req.Config.TenantID, time.Since(startTime))
		}

		s.logger.DebugContext(ctx,
			"no suitable node found for agent",
			"agent_id", req.Config.ID,
			"error", err,
		)

		// Send event about failed scheduling
		s.sendEvent(ctx, ScheduleEvent{
			AgentID:   req.Config.ID,
			Success:   false,
			Error:     err,
			Timestamp: time.Now(),
		})
		return
	}

	span.SetAttributes(
		attribute.String("node.id", node.ID),
		attribute.Float64("node.utilization_before", node.UtilizationPercent()),
	)

	// Verify node can still fit (double-check under lock to prevent race conditions)
	if !node.CanFit(req.Resources) {
		// Node capacity changed between selection and now, retry later
		span.SetStatus(codes.Error, "node capacity changed")
		span.AddEvent("node_capacity_changed")

		// Record scheduling error metric
		if s.metrics != nil {
			s.metrics.RecordSchedulingError("node_capacity_changed", req.Config.TenantID)
			s.metrics.RecordSchedulingDuration(string(s.placer.strategy), req.Config.TenantID, time.Since(startTime))
		}

		s.logger.DebugContext(ctx,
			"node capacity changed, agent no longer fits",
			"agent_id", req.Config.ID,
			"node_id", node.ID,
		)

		s.sendEvent(ctx, ScheduleEvent{
			AgentID:   req.Config.ID,
			Success:   false,
			Error:     fmt.Errorf("node capacity changed"),
			Timestamp: time.Now(),
		})
		return
	}

	// Allocate resources on the node (still under scheduler lock)
	node.Allocate(req.Config.ID, req.Config.TenantID, req.Resources)

	// Remove from queue (successfully scheduled)
	s.queue.Dequeue()

	// Record successful scheduling metric
	if s.metrics != nil {
		s.metrics.RecordSchedulingDuration(string(s.placer.strategy), req.Config.TenantID, time.Since(startTime))
	}

	span.SetAttributes(
		attribute.Bool("success", true),
		attribute.Float64("node.utilization_after", node.UtilizationPercent()),
	)

	s.logger.InfoContext(ctx,
		"scheduled agent",
		"agent_id", req.Config.ID,
		"node_id", node.ID,
		"utilization", fmt.Sprintf("%.1f%%", node.UtilizationPercent()),
	)

	// Send event about successful scheduling
	s.sendEvent(ctx, ScheduleEvent{
		AgentID:   req.Config.ID,
		NodeID:    node.ID,
		Success:   true,
		Timestamp: time.Now(),
	})
}

// sendEvent sends a scheduling event to the event channel (non-blocking).
func (s *Scheduler) sendEvent(ctx context.Context, event ScheduleEvent) {
	select {
	case s.events <- event:
	default:
		// Event channel is full, log and drop
		s.logger.WarnContext(ctx,
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
