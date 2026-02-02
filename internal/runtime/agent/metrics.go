// Package agent provides agent lifecycle management and abstraction.
package agent

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/aether-runtime/aether/pkg/api"
)

// MetricsCollector periodically collects metrics from an agent's VM.
type MetricsCollector struct {
	logger   *slog.Logger
	agentID  string
	interval time.Duration

	mu      sync.RWMutex
	latest  *api.AgentMetrics
	stopCh  chan struct{}
	stopped bool
}

// NewMetricsCollector creates a new metrics collector.
func NewMetricsCollector(logger *slog.Logger, agentID string) *MetricsCollector {
	return &MetricsCollector{
		logger:   logger.With("component", "metrics_collector", "agent_id", agentID),
		agentID:  agentID,
		interval: 5 * time.Second, // Collect metrics every 5 seconds
		stopCh:   make(chan struct{}),
	}
}

// Start begins collecting metrics from the VM.
func (mc *MetricsCollector) Start(ctx context.Context, vm VM) {
	mc.mu.Lock()
	if !mc.stopped {
		mc.mu.Unlock()
		return // Already running
	}
	mc.stopped = false
	mc.stopCh = make(chan struct{})
	mc.mu.Unlock()

	go mc.collectLoop(ctx, vm)
}

// Stop stops the metrics collector.
func (mc *MetricsCollector) Stop() {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	if mc.stopped {
		return
	}

	mc.stopped = true
	close(mc.stopCh)
}

// GetLatest returns the latest collected metrics.
func (mc *MetricsCollector) GetLatest() *api.AgentMetrics {
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	if mc.latest == nil {
		return nil
	}

	// Return a copy to avoid race conditions
	metricsCopy := *mc.latest
	return &metricsCopy
}

// collectLoop is the main collection loop.
func (mc *MetricsCollector) collectLoop(ctx context.Context, vm VM) {
	ticker := time.NewTicker(mc.interval)
	defer ticker.Stop()

	mc.logger.DebugContext(ctx, "metrics collector started")

	for {
		select {
		case <-mc.stopCh:
			mc.logger.DebugContext(ctx, "metrics collector stopped")
			return
		case <-ctx.Done():
			mc.logger.DebugContext(ctx, "metrics collector stopped due to context cancellation")
			return
		case <-ticker.C:
			if err := mc.collect(ctx, vm); err != nil {
				mc.logger.WarnContext(ctx, "failed to collect metrics", "error", err)
			}
		}
	}
}

// collect retrieves metrics from the VM and updates the latest metrics.
func (mc *MetricsCollector) collect(ctx context.Context, vm VM) error {
	metrics, err := vm.GetMetrics(ctx)
	if err != nil {
		return err
	}

	mc.mu.Lock()
	mc.latest = metrics
	mc.mu.Unlock()

	mc.logger.DebugContext(ctx,
		"collected metrics",
		"cpu_percent", metrics.CPUUsagePercent,
		"memory_mb", metrics.MemoryUsageMB,
	)

	return nil
}
