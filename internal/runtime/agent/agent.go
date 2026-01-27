// Package agent provides agent lifecycle management and abstraction.
package agent

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/dnakitare/aether/pkg/api"
)

// Agent represents a running agent instance.
type Agent struct {
	mu     sync.RWMutex
	info   api.AgentInfo
	logger *slog.Logger

	// vm is the underlying VM instance (interface for testability)
	vm VM

	// stopChan is closed when the agent should stop
	stopChan chan struct{}

	// metrics collector
	metricsCollector *MetricsCollector
}

// VM is an interface for the underlying VM implementation.
// This allows for easier testing and potential alternative backends.
type VM interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context, timeout time.Duration) error
	Destroy(ctx context.Context) error
	IsRunning() bool
	GetMetrics(ctx context.Context) (*api.AgentMetrics, error)
}

// New creates a new agent instance.
func New(logger *slog.Logger, config api.AgentConfig, vm VM) *Agent {
	now := time.Now()

	return &Agent{
		info: api.AgentInfo{
			Config:    config,
			Status:    api.AgentStatusPending,
			CreatedAt: now,
		},
		logger:           logger.With("agent_id", config.ID, "tenant_id", config.TenantID),
		vm:               vm,
		stopChan:         make(chan struct{}),
		metricsCollector: NewMetricsCollector(logger, string(config.ID)),
	}
}

// Start starts the agent.
func (a *Agent) Start(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.info.Status != api.AgentStatusPending && a.info.Status != api.AgentStatusStopped {
		return fmt.Errorf("cannot start agent in status %s", a.info.Status)
	}

	a.logger.InfoContext(ctx, "starting agent")
	a.info.Status = api.AgentStatusCreating

	// Start the underlying VM
	if err := a.vm.Start(ctx); err != nil {
		a.info.Status = api.AgentStatusFailed
		a.info.Error = err.Error()
		return fmt.Errorf("failed to start VM: %w", err)
	}

	now := time.Now()
	a.info.Status = api.AgentStatusRunning
	a.info.StartedAt = &now
	a.info.Error = ""

	// Start metrics collection
	a.metricsCollector.Start(ctx, a.vm)

	a.logger.InfoContext(ctx, "agent started successfully")
	return nil
}

// Stop stops the agent gracefully.
func (a *Agent) Stop(ctx context.Context, timeout time.Duration) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.info.Status != api.AgentStatusRunning {
		return fmt.Errorf("cannot stop agent in status %s", a.info.Status)
	}

	a.logger.InfoContext(ctx, "stopping agent", "timeout", timeout)
	a.info.Status = api.AgentStatusStopping

	// Stop metrics collection
	a.metricsCollector.Stop()

	// Stop the VM
	if err := a.vm.Stop(ctx, timeout); err != nil {
		a.logger.WarnContext(ctx, "error stopping VM", "error", err)
		// Continue with cleanup even if stop fails
	}

	now := time.Now()
	a.info.Status = api.AgentStatusStopped
	a.info.StoppedAt = &now

	close(a.stopChan)

	a.logger.InfoContext(ctx, "agent stopped")
	return nil
}

// Destroy destroys the agent and cleans up all resources.
func (a *Agent) Destroy(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.logger.InfoContext(ctx, "destroying agent")

	// Ensure agent is stopped first
	if a.info.Status == api.AgentStatusRunning {
		a.metricsCollector.Stop()
		if err := a.vm.Stop(ctx, 5*time.Second); err != nil {
			a.logger.WarnContext(ctx, "error stopping VM during destroy", "error", err)
		}
	}

	// Destroy the VM
	if err := a.vm.Destroy(ctx); err != nil {
		return fmt.Errorf("failed to destroy VM: %w", err)
	}

	a.logger.InfoContext(ctx, "agent destroyed")
	return nil
}

// GetInfo returns the current agent information.
func (a *Agent) GetInfo() api.AgentInfo {
	a.mu.RLock()
	defer a.mu.RUnlock()

	// Get latest metrics if running
	if a.info.Status == api.AgentStatusRunning {
		a.info.Metrics = a.metricsCollector.GetLatest()
	}

	return a.info
}

// GetHealth checks the agent's health.
func (a *Agent) GetHealth(ctx context.Context) *api.HealthStatus {
	a.mu.RLock()
	defer a.mu.RUnlock()

	now := time.Now()

	if a.info.Status != api.AgentStatusRunning {
		return &api.HealthStatus{
			Healthy:   false,
			Message:   fmt.Sprintf("agent is not running (status: %s)", a.info.Status),
			CheckedAt: now,
		}
	}

	if !a.vm.IsRunning() {
		return &api.HealthStatus{
			Healthy:   false,
			Message:   "VM is not running",
			CheckedAt: now,
		}
	}

	return &api.HealthStatus{
		Healthy:   true,
		Message:   "agent is healthy",
		CheckedAt: now,
	}
}

// UpdateStatus updates the agent's status (used internally).
func (a *Agent) UpdateStatus(status api.AgentStatus, errMsg string) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.info.Status = status
	if errMsg != "" {
		a.info.Error = errMsg
	}

	if status == api.AgentStatusStopped || status == api.AgentStatusFailed {
		now := time.Now()
		a.info.StoppedAt = &now
	}
}
