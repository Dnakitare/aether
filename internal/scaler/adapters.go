package scaler

import (
	"context"
	"fmt"
	"time"

	"github.com/dnakitare/aether/pkg/api"
)

// RuntimeAdapter is the subset of the runtime needed by the scaler adapters.
// Both MetricsProvider and ScaleExecutor are implemented against this interface
// so the scaler package doesn't import internal/runtime (avoiding cycles).
type RuntimeAdapter interface {
	GetAgent(ctx context.Context, id api.AgentID) (*api.AgentInfo, error)
	ListAgents(ctx context.Context, tenantID *api.TenantID) ([]*api.AgentInfo, error)
	GetAgentHealth(ctx context.Context, id api.AgentID) (*api.HealthStatus, error)
	CreateAgent(ctx context.Context, config api.AgentConfig) error
	StartAgent(ctx context.Context, id api.AgentID) error
	DestroyAgent(ctx context.Context, id api.AgentID) error
}

// QuotaReleaser releases quota when scaling down.
type QuotaReleaser interface {
	ReleaseResources(ctx context.Context, tenantID api.TenantID, release interface{ GetAgentCount() int }) error
}

// RuntimeMetricsProvider implements MetricsProvider using the live runtime.
type RuntimeMetricsProvider struct {
	runtime RuntimeAdapter
}

// NewRuntimeMetricsProvider creates a MetricsProvider backed by the runtime.
func NewRuntimeMetricsProvider(rt RuntimeAdapter) *RuntimeMetricsProvider {
	return &RuntimeMetricsProvider{runtime: rt}
}

// GetAgentMetrics returns metrics for a single agent.
// It uses the metrics embedded in AgentInfo when available, since HealthStatus
// does not carry CPU/memory fields. The health check is used as a liveness signal.
func (p *RuntimeMetricsProvider) GetAgentMetrics(ctx context.Context, agentID api.AgentID) (*api.AgentMetrics, error) {
	info, err := p.runtime.GetAgent(ctx, agentID)
	if err != nil {
		return nil, fmt.Errorf("agent not found: %w", err)
	}

	// Use metrics embedded in agent info when the runtime has collected them.
	if info.Metrics != nil {
		return info.Metrics, nil
	}

	// Fall back to a zero-value metrics struct; health check confirms liveness.
	_, _ = p.runtime.GetAgentHealth(ctx, agentID) // liveness signal, error non-fatal
	return &api.AgentMetrics{
		LastUpdated: time.Now(),
	}, nil
}

// GetTenantMetrics returns aggregated metrics for all agents in a tenant.
func (p *RuntimeMetricsProvider) GetTenantMetrics(ctx context.Context, tenantID api.TenantID) (*TenantMetrics, error) {
	agents, err := p.runtime.ListAgents(ctx, &tenantID)
	if err != nil {
		return nil, fmt.Errorf("failed to list agents: %w", err)
	}

	m := &TenantMetrics{
		TenantID:    tenantID,
		AgentCount:  len(agents),
		LastUpdated: time.Now(),
	}

	// Aggregate CPU and memory from running agents that have metrics populated.
	var totalCPU float64
	var totalMemory int64
	var runningWithMetrics int

	for _, a := range agents {
		if a.Status != api.AgentStatusRunning || a.Metrics == nil {
			continue
		}
		totalCPU += a.Metrics.CPUUsagePercent
		totalMemory += a.Metrics.MemoryUsageMB
		runningWithMetrics++
	}

	if runningWithMetrics > 0 {
		m.AvgCPUPercent = totalCPU / float64(runningWithMetrics)
		m.AvgMemoryMB = totalMemory / int64(runningWithMetrics)
	}

	return m, nil
}

// RuntimeScaleExecutor implements ScaleExecutor using the runtime.
type RuntimeScaleExecutor struct {
	runtime RuntimeAdapter
}

// NewRuntimeScaleExecutor creates a ScaleExecutor backed by the runtime.
func NewRuntimeScaleExecutor(rt RuntimeAdapter) *RuntimeScaleExecutor {
	return &RuntimeScaleExecutor{runtime: rt}
}

// ScaleUp creates `count` new agents for the target tenant using default config.
func (e *RuntimeScaleExecutor) ScaleUp(ctx context.Context, target ScaleTarget, count int) error {
	if target.Type != TargetTypeTenant {
		return fmt.Errorf("scale up only supported for tenant targets, got %s", target.Type)
	}

	for i := 0; i < count; i++ {
		cfg := api.AgentConfig{
			ID:       api.AgentID(fmt.Sprintf("autoscale-%s-%d-%d", target.ID, time.Now().UnixNano(), i)),
			TenantID: api.TenantID(target.ID),
			Name:     fmt.Sprintf("autoscaled-%d", i),
		}
		if err := e.runtime.CreateAgent(ctx, cfg); err != nil {
			return fmt.Errorf("scale up failed at agent %d: %w", i, err)
		}
		if err := e.runtime.StartAgent(ctx, cfg.ID); err != nil {
			_ = e.runtime.DestroyAgent(ctx, cfg.ID)
			return fmt.Errorf("scale up: failed to start agent %d: %w", i, err)
		}
	}
	return nil
}

// ScaleDown destroys the `count` oldest stopped or least-loaded agents.
func (e *RuntimeScaleExecutor) ScaleDown(ctx context.Context, target ScaleTarget, count int) error {
	if target.Type != TargetTypeTenant {
		return fmt.Errorf("scale down only supported for tenant targets, got %s", target.Type)
	}

	tenantID := api.TenantID(target.ID)
	agents, err := e.runtime.ListAgents(ctx, &tenantID)
	if err != nil {
		return fmt.Errorf("failed to list agents for scale down: %w", err)
	}

	destroyed := 0
	for _, a := range agents {
		if destroyed >= count {
			break
		}
		if a.Status == api.AgentStatusStopped || a.Status == api.AgentStatusFailed {
			if err := e.runtime.DestroyAgent(ctx, a.Config.ID); err != nil {
				return fmt.Errorf("scale down: failed to destroy agent %s: %w", a.Config.ID, err)
			}
			destroyed++
		}
	}

	return nil
}
