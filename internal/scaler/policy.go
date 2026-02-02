// Package scaler provides auto-scaling functionality for agents.
package scaler

import (
	"fmt"
	"time"

	"github.com/aether-runtime/aether/pkg/api"
)

// TargetType represents what the scaling policy targets.
type TargetType string

const (
	// TargetTypeTenant targets all agents in a tenant.
	TargetTypeTenant TargetType = "tenant"

	// TargetTypeAgent targets a specific agent.
	TargetTypeAgent TargetType = "agent"
)

// ScaleTarget identifies what to scale.
type ScaleTarget struct {
	Type TargetType
	ID   string // Tenant ID or Agent ID
}

// String returns a string representation of the target.
func (st ScaleTarget) String() string {
	return fmt.Sprintf("%s/%s", st.Type, st.ID)
}

// Policy defines an auto-scaling policy.
type Policy struct {
	// Name is a unique identifier for this policy
	Name string

	// Target specifies what to scale
	Target ScaleTarget

	// ScaleUp defines when and how to scale up
	ScaleUp ScaleAction

	// ScaleDown defines when and how to scale down
	ScaleDown ScaleAction

	// MinReplicas is the minimum number of agents
	MinReplicas int

	// MaxReplicas is the maximum number of agents
	MaxReplicas int

	// Cooldown is the minimum time between scaling actions
	Cooldown time.Duration

	// Enabled indicates if this policy is active
	Enabled bool
}

// ScaleAction defines a scaling action.
type ScaleAction struct {
	// Rules are conditions that trigger this action
	Rules []ScaleRule

	// Delta is how many agents to add/remove
	Delta int
}

// ScaleRule defines a condition for scaling.
type ScaleRule struct {
	// Metric to evaluate
	Metric MetricType

	// Threshold value
	Threshold float64

	// Operator for comparison
	Operator ComparisonOperator

	// Duration that condition must be true
	Duration time.Duration
}

// MetricType represents a metric to monitor.
type MetricType string

const (
	// MetricCPUPercent is CPU utilization percentage (0-100)
	MetricCPUPercent MetricType = "cpu_percent"

	// MetricMemoryPercent is memory utilization percentage (0-100)
	MetricMemoryPercent MetricType = "memory_percent"

	// MetricRequestRate is requests per second
	MetricRequestRate MetricType = "request_rate"

	// MetricAgentCount is the number of agents
	MetricAgentCount MetricType = "agent_count"
)

// ComparisonOperator defines how to compare metric to threshold.
type ComparisonOperator string

const (
	// OperatorGreaterThan checks if metric > threshold
	OperatorGreaterThan ComparisonOperator = ">"

	// OperatorLessThan checks if metric < threshold
	OperatorLessThan ComparisonOperator = "<"

	// OperatorGreaterOrEqual checks if metric >= threshold
	OperatorGreaterOrEqual ComparisonOperator = ">="

	// OperatorLessOrEqual checks if metric <= threshold
	OperatorLessOrEqual ComparisonOperator = "<="
)

// Validate checks if the policy is valid.
func (p *Policy) Validate() error {
	if p.Name == "" {
		return fmt.Errorf("policy name is required")
	}

	if p.Target.Type == "" || p.Target.ID == "" {
		return fmt.Errorf("policy target is required")
	}

	if p.MinReplicas < 0 {
		return fmt.Errorf("min replicas must be >= 0")
	}

	if p.MaxReplicas < p.MinReplicas {
		return fmt.Errorf("max replicas must be >= min replicas")
	}

	if p.ScaleUp.Delta <= 0 {
		return fmt.Errorf("scale up delta must be > 0")
	}

	if p.ScaleDown.Delta <= 0 {
		return fmt.Errorf("scale down delta must be > 0")
	}

	if len(p.ScaleUp.Rules) == 0 && len(p.ScaleDown.Rules) == 0 {
		return fmt.Errorf("at least one scale rule is required")
	}

	return nil
}

// Evaluate evaluates a rule against tenant metrics.
func (r *ScaleRule) Evaluate(metrics *TenantMetrics) bool {
	var value float64

	switch r.Metric {
	case MetricCPUPercent:
		value = metrics.AvgCPUPercent
	case MetricMemoryPercent:
		// Calculate memory percentage (would need capacity info in real impl)
		value = 0 // Placeholder
	case MetricRequestRate:
		value = metrics.RequestRate
	case MetricAgentCount:
		value = float64(metrics.AgentCount)
	default:
		return false
	}

	return r.compare(value)
}

// EvaluateAgent evaluates a rule against agent metrics.
func (r *ScaleRule) EvaluateAgent(metrics *api.AgentMetrics) bool {
	var value float64

	switch r.Metric {
	case MetricCPUPercent:
		value = metrics.CPUUsagePercent
	case MetricMemoryPercent:
		// Would need to calculate percentage based on limits
		value = 0 // Placeholder
	default:
		return false
	}

	return r.compare(value)
}

// compare performs the comparison based on the operator.
func (r *ScaleRule) compare(value float64) bool {
	switch r.Operator {
	case OperatorGreaterThan:
		return value > r.Threshold
	case OperatorLessThan:
		return value < r.Threshold
	case OperatorGreaterOrEqual:
		return value >= r.Threshold
	case OperatorLessOrEqual:
		return value <= r.Threshold
	default:
		return false
	}
}

// NewCPUPolicy creates a simple CPU-based scaling policy.
func NewCPUPolicy(name string, target ScaleTarget, scaleUpAt, scaleDownAt float64) *Policy {
	return &Policy{
		Name:   name,
		Target: target,
		ScaleUp: ScaleAction{
			Rules: []ScaleRule{
				{
					Metric:    MetricCPUPercent,
					Threshold: scaleUpAt,
					Operator:  OperatorGreaterThan,
					Duration:  1 * time.Minute,
				},
			},
			Delta: 1,
		},
		ScaleDown: ScaleAction{
			Rules: []ScaleRule{
				{
					Metric:    MetricCPUPercent,
					Threshold: scaleDownAt,
					Operator:  OperatorLessThan,
					Duration:  5 * time.Minute,
				},
			},
			Delta: 1,
		},
		MinReplicas: 1,
		MaxReplicas: 10,
		Cooldown:    5 * time.Minute,
		Enabled:     true,
	}
}
