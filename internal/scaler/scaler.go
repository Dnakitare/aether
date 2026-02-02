// Package scaler provides auto-scaling functionality for agents.
package scaler

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/aether-runtime/aether/pkg/api"
)

// Scaler manages automatic scaling of agents based on metrics.
type Scaler struct {
	logger *slog.Logger
	mu     sync.RWMutex

	// Policies define scaling rules
	policies map[string]*Policy

	// MetricsProvider supplies metrics for scaling decisions
	metricsProvider MetricsProvider

	// ScaleExecutor executes scale actions
	executor ScaleExecutor

	// Stop channel
	stopCh chan struct{}

	// Evaluation interval
	interval time.Duration

	// Cooldown tracker
	cooldowns map[string]time.Time
}

// MetricsProvider provides metrics for scaling decisions.
type MetricsProvider interface {
	GetAgentMetrics(ctx context.Context, agentID api.AgentID) (*api.AgentMetrics, error)
	GetTenantMetrics(ctx context.Context, tenantID api.TenantID) (*TenantMetrics, error)
}

// ScaleExecutor executes scaling actions.
type ScaleExecutor interface {
	ScaleUp(ctx context.Context, target ScaleTarget, count int) error
	ScaleDown(ctx context.Context, target ScaleTarget, count int) error
}

// TenantMetrics contains aggregated metrics for a tenant.
type TenantMetrics struct {
	TenantID      api.TenantID
	AgentCount    int
	AvgCPUPercent float64
	AvgMemoryMB   int64
	TotalRequests int64
	RequestRate   float64 // Requests per second
	LastUpdated   time.Time
}

// Config holds scaler configuration.
type Config struct {
	// Interval between evaluation cycles
	Interval time.Duration

	// DefaultCooldown is the minimum time between scale actions
	DefaultCooldown time.Duration
}

// New creates a new auto-scaler.
func New(logger *slog.Logger, config Config, metricsProvider MetricsProvider, executor ScaleExecutor) *Scaler {
	if config.Interval == 0 {
		config.Interval = 30 * time.Second
	}
	if config.DefaultCooldown == 0 {
		config.DefaultCooldown = 5 * time.Minute
	}

	return &Scaler{
		logger:          logger.With("component", "scaler"),
		policies:        make(map[string]*Policy),
		metricsProvider: metricsProvider,
		executor:        executor,
		stopCh:          make(chan struct{}),
		interval:        config.Interval,
		cooldowns:       make(map[string]time.Time),
	}
}

// Start begins the auto-scaling loop.
func (s *Scaler) Start(ctx context.Context) {
	s.logger.InfoContext(ctx, "starting auto-scaler", "interval", s.interval)

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			s.logger.InfoContext(ctx, "scaler stopping due to context cancellation")
			return
		case <-s.stopCh:
			s.logger.InfoContext(ctx, "scaler stopped")
			return
		case <-ticker.C:
			s.evaluate(ctx)
		}
	}
}

// Stop stops the auto-scaler.
func (s *Scaler) Stop() {
	close(s.stopCh)
}

// AddPolicy adds a scaling policy.
func (s *Scaler) AddPolicy(policy *Policy) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := policy.Validate(); err != nil {
		return fmt.Errorf("invalid policy: %w", err)
	}

	s.policies[policy.Name] = policy
	s.logger.InfoContext(context.Background(),
		"added scaling policy",
		"name", policy.Name,
		"target", policy.Target.String(),
	)

	return nil
}

// RemovePolicy removes a scaling policy.
func (s *Scaler) RemovePolicy(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.policies, name)
	delete(s.cooldowns, name)
	s.logger.InfoContext(context.Background(), "removed scaling policy", "name", name)
}

// GetPolicy retrieves a policy by name.
func (s *Scaler) GetPolicy(name string) (*Policy, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	policy, exists := s.policies[name]
	return policy, exists
}

// ListPolicies returns all policies.
func (s *Scaler) ListPolicies() []*Policy {
	s.mu.RLock()
	defer s.mu.RUnlock()

	policies := make([]*Policy, 0, len(s.policies))
	for _, policy := range s.policies {
		policies = append(policies, policy)
	}
	return policies
}

// evaluate checks all policies and triggers scaling actions.
func (s *Scaler) evaluate(ctx context.Context) {
	s.mu.RLock()
	policies := make([]*Policy, 0, len(s.policies))
	for _, policy := range s.policies {
		if policy.Enabled {
			policies = append(policies, policy)
		}
	}
	s.mu.RUnlock()

	for _, policy := range policies {
		if err := s.evaluatePolicy(ctx, policy); err != nil {
			s.logger.ErrorContext(ctx,
				"failed to evaluate policy",
				"policy", policy.Name,
				"error", err,
			)
		}
	}
}

// evaluatePolicy evaluates a single policy and triggers scaling if needed.
func (s *Scaler) evaluatePolicy(ctx context.Context, policy *Policy) error {
	// Check cooldown
	if s.isInCooldown(policy.Name) {
		s.logger.DebugContext(ctx, "policy in cooldown, skipping", "policy", policy.Name)
		return nil
	}

	// Get metrics based on target type
	var shouldScale bool
	var scaleUp bool
	var delta int
	var err error

	switch policy.Target.Type {
	case TargetTypeTenant:
		shouldScale, scaleUp, delta, err = s.evaluateTenantPolicy(ctx, policy)
	case TargetTypeAgent:
		shouldScale, scaleUp, delta, err = s.evaluateAgentPolicy(ctx, policy)
	default:
		return fmt.Errorf("unknown target type: %s", policy.Target.Type)
	}

	if err != nil {
		return err
	}

	if !shouldScale {
		return nil
	}

	// Execute scaling action
	if scaleUp {
		s.logger.InfoContext(ctx,
			"scaling up",
			"policy", policy.Name,
			"target", policy.Target.String(),
			"delta", delta,
		)
		if err := s.executor.ScaleUp(ctx, policy.Target, delta); err != nil {
			return fmt.Errorf("scale up failed: %w", err)
		}
	} else {
		s.logger.InfoContext(ctx,
			"scaling down",
			"policy", policy.Name,
			"target", policy.Target.String(),
			"delta", delta,
		)
		if err := s.executor.ScaleDown(ctx, policy.Target, delta); err != nil {
			return fmt.Errorf("scale down failed: %w", err)
		}
	}

	// Set cooldown
	s.setCooldown(policy.Name, policy.Cooldown)

	return nil
}

// evaluateTenantPolicy evaluates a tenant-level scaling policy.
func (s *Scaler) evaluateTenantPolicy(ctx context.Context, policy *Policy) (shouldScale bool, scaleUp bool, delta int, err error) {
	metrics, err := s.metricsProvider.GetTenantMetrics(ctx, api.TenantID(policy.Target.ID))
	if err != nil {
		return false, false, 0, fmt.Errorf("failed to get tenant metrics: %w", err)
	}

	// Check scale up conditions
	for _, rule := range policy.ScaleUp.Rules {
		if rule.Evaluate(metrics) {
			shouldScale = true
			scaleUp = true
			delta = policy.ScaleUp.Delta
			return
		}
	}

	// Check scale down conditions
	for _, rule := range policy.ScaleDown.Rules {
		if rule.Evaluate(metrics) {
			shouldScale = true
			scaleUp = false
			delta = policy.ScaleDown.Delta
			return
		}
	}

	return false, false, 0, nil
}

// evaluateAgentPolicy evaluates an agent-level scaling policy.
func (s *Scaler) evaluateAgentPolicy(ctx context.Context, policy *Policy) (shouldScale bool, scaleUp bool, delta int, err error) {
	metrics, err := s.metricsProvider.GetAgentMetrics(ctx, api.AgentID(policy.Target.ID))
	if err != nil {
		return false, false, 0, fmt.Errorf("failed to get agent metrics: %w", err)
	}

	// Check scale up conditions
	for _, rule := range policy.ScaleUp.Rules {
		if rule.EvaluateAgent(metrics) {
			shouldScale = true
			scaleUp = true
			delta = policy.ScaleUp.Delta
			return
		}
	}

	// Check scale down conditions
	for _, rule := range policy.ScaleDown.Rules {
		if rule.EvaluateAgent(metrics) {
			shouldScale = true
			scaleUp = false
			delta = policy.ScaleDown.Delta
			return
		}
	}

	return false, false, 0, nil
}

// isInCooldown checks if a policy is in cooldown period.
func (s *Scaler) isInCooldown(policyName string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	lastAction, exists := s.cooldowns[policyName]
	if !exists {
		return false
	}

	return time.Since(lastAction) < time.Minute // Simplified check
}

// setCooldown sets the cooldown for a policy.
func (s *Scaler) setCooldown(policyName string, duration time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.cooldowns[policyName] = time.Now().Add(duration)
}
