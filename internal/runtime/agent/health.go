// Package agent provides agent lifecycle management and abstraction.
package agent

import (
	"context"
	"fmt"
	"time"

	"github.com/dnakitare/aether/pkg/api"
)

// HealthChecker performs health checks on agents.
type HealthChecker struct {
	timeout time.Duration
}

// NewHealthChecker creates a new health checker.
func NewHealthChecker(timeout time.Duration) *HealthChecker {
	return &HealthChecker{
		timeout: timeout,
	}
}

// Check performs a health check on an agent.
func (hc *HealthChecker) Check(ctx context.Context, agent *Agent) (*api.HealthStatus, error) {
	ctx, cancel := context.WithTimeout(ctx, hc.timeout)
	defer cancel()

	// Use the agent's built-in health check
	health := agent.GetHealth(ctx)

	if !health.Healthy {
		return health, fmt.Errorf("agent is unhealthy: %s", health.Message)
	}

	return health, nil
}

// CheckMultiple performs health checks on multiple agents concurrently.
func (hc *HealthChecker) CheckMultiple(ctx context.Context, agents []*Agent) map[api.AgentID]*api.HealthStatus {
	results := make(map[api.AgentID]*api.HealthStatus)
	resultsCh := make(chan struct {
		id     api.AgentID
		health *api.HealthStatus
	}, len(agents))

	// Check all agents concurrently
	for _, agent := range agents {
		go func(a *Agent) {
			health := a.GetHealth(ctx)
			resultsCh <- struct {
				id     api.AgentID
				health *api.HealthStatus
			}{
				id:     a.info.Config.ID,
				health: health,
			}
		}(agent)
	}

	// Collect results
	for i := 0; i < len(agents); i++ {
		result := <-resultsCh
		results[result.id] = result.health
	}

	return results
}
