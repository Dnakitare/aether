// Package scaler provides auto-scaling functionality for agents.
package scaler

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/dnakitare/aether/pkg/api"
)

// SimpleMetricsProvider is a basic in-memory metrics provider for testing.
type SimpleMetricsProvider struct {
	mu            sync.RWMutex
	agentMetrics  map[api.AgentID]*api.AgentMetrics
	tenantMetrics map[api.TenantID]*TenantMetrics
}

// NewSimpleMetricsProvider creates a new simple metrics provider.
func NewSimpleMetricsProvider() *SimpleMetricsProvider {
	return &SimpleMetricsProvider{
		agentMetrics:  make(map[api.AgentID]*api.AgentMetrics),
		tenantMetrics: make(map[api.TenantID]*TenantMetrics),
	}
}

// GetAgentMetrics retrieves metrics for an agent.
func (p *SimpleMetricsProvider) GetAgentMetrics(ctx context.Context, agentID api.AgentID) (*api.AgentMetrics, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	metrics, exists := p.agentMetrics[agentID]
	if !exists {
		return nil, fmt.Errorf("agent %s not found", agentID)
	}

	return metrics, nil
}

// GetTenantMetrics retrieves aggregated metrics for a tenant.
func (p *SimpleMetricsProvider) GetTenantMetrics(ctx context.Context, tenantID api.TenantID) (*TenantMetrics, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	metrics, exists := p.tenantMetrics[tenantID]
	if !exists {
		return nil, fmt.Errorf("tenant %s not found", tenantID)
	}

	return metrics, nil
}

// UpdateAgentMetrics updates metrics for an agent.
func (p *SimpleMetricsProvider) UpdateAgentMetrics(agentID api.AgentID, metrics *api.AgentMetrics) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.agentMetrics[agentID] = metrics
}

// UpdateTenantMetrics updates metrics for a tenant.
func (p *SimpleMetricsProvider) UpdateTenantMetrics(tenantID api.TenantID, metrics *TenantMetrics) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.tenantMetrics[tenantID] = metrics
}

// MockScaleExecutor is a mock executor for testing.
type MockScaleExecutor struct {
	mu             sync.RWMutex
	scaleUpCalls   []ScaleCall
	scaleDownCalls []ScaleCall
}

// ScaleCall records a scaling action.
type ScaleCall struct {
	Target    ScaleTarget
	Count     int
	Timestamp time.Time
}

// NewMockScaleExecutor creates a new mock scale executor.
func NewMockScaleExecutor() *MockScaleExecutor {
	return &MockScaleExecutor{
		scaleUpCalls:   make([]ScaleCall, 0),
		scaleDownCalls: make([]ScaleCall, 0),
	}
}

// ScaleUp records a scale up action.
func (e *MockScaleExecutor) ScaleUp(ctx context.Context, target ScaleTarget, count int) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.scaleUpCalls = append(e.scaleUpCalls, ScaleCall{
		Target:    target,
		Count:     count,
		Timestamp: time.Now(),
	})

	return nil
}

// ScaleDown records a scale down action.
func (e *MockScaleExecutor) ScaleDown(ctx context.Context, target ScaleTarget, count int) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.scaleDownCalls = append(e.scaleDownCalls, ScaleCall{
		Target:    target,
		Count:     count,
		Timestamp: time.Now(),
	})

	return nil
}

// GetScaleUpCalls returns all scale up calls.
func (e *MockScaleExecutor) GetScaleUpCalls() []ScaleCall {
	e.mu.RLock()
	defer e.mu.RUnlock()

	calls := make([]ScaleCall, len(e.scaleUpCalls))
	copy(calls, e.scaleUpCalls)
	return calls
}

// GetScaleDownCalls returns all scale down calls.
func (e *MockScaleExecutor) GetScaleDownCalls() []ScaleCall {
	e.mu.RLock()
	defer e.mu.RUnlock()

	calls := make([]ScaleCall, len(e.scaleDownCalls))
	copy(calls, e.scaleDownCalls)
	return calls
}

// Reset clears all recorded calls.
func (e *MockScaleExecutor) Reset() {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.scaleUpCalls = make([]ScaleCall, 0)
	e.scaleDownCalls = make([]ScaleCall, 0)
}
