package scaler_test

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/aether-runtime/aether/internal/scaler"
	"github.com/aether-runtime/aether/pkg/api"
)

func TestScaler(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	metricsProvider := scaler.NewSimpleMetricsProvider()
	executor := scaler.NewMockScaleExecutor()

	config := scaler.Config{
		Interval:        100 * time.Millisecond,
		DefaultCooldown: 1 * time.Second,
	}

	s := scaler.New(logger, config, metricsProvider, executor)

	// Add a CPU-based scaling policy
	policy := scaler.NewCPUPolicy(
		"test-policy",
		scaler.ScaleTarget{Type: scaler.TargetTypeTenant, ID: "tenant-1"},
		80.0, // Scale up at 80% CPU
		20.0, // Scale down at 20% CPU
	)

	err := s.AddPolicy(policy)
	if err != nil {
		t.Fatalf("Failed to add policy: %v", err)
	}

	// Set high CPU to trigger scale up
	metricsProvider.UpdateTenantMetrics(api.TenantID("tenant-1"), &scaler.TenantMetrics{
		TenantID:      api.TenantID("tenant-1"),
		AgentCount:    2,
		AvgCPUPercent: 85.0,
		LastUpdated:   time.Now(),
	})

	// Start scaler
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go s.Start(ctx)

	// Wait for scale up
	time.Sleep(300 * time.Millisecond)

	scaleUpCalls := executor.GetScaleUpCalls()
	if len(scaleUpCalls) == 0 {
		t.Error("Expected scale up call but got none")
	} else if scaleUpCalls[0].Count != 1 {
		t.Errorf("Scale up count = %d, want 1", scaleUpCalls[0].Count)
	}
}

func TestPolicyValidation(t *testing.T) {
	tests := []struct {
		name    string
		policy  *scaler.Policy
		wantErr bool
	}{
		{
			name: "valid policy",
			policy: &scaler.Policy{
				Name:   "valid",
				Target: scaler.ScaleTarget{Type: scaler.TargetTypeTenant, ID: "tenant-1"},
				ScaleUp: scaler.ScaleAction{
					Rules: []scaler.ScaleRule{
						{Metric: scaler.MetricCPUPercent, Threshold: 80, Operator: scaler.OperatorGreaterThan},
					},
					Delta: 1,
				},
				ScaleDown: scaler.ScaleAction{
					Rules: []scaler.ScaleRule{
						{Metric: scaler.MetricCPUPercent, Threshold: 20, Operator: scaler.OperatorLessThan},
					},
					Delta: 1,
				},
				MinReplicas: 1,
				MaxReplicas: 10,
			},
			wantErr: false,
		},
		{
			name: "missing name",
			policy: &scaler.Policy{
				Target: scaler.ScaleTarget{Type: scaler.TargetTypeTenant, ID: "tenant-1"},
				ScaleUp: scaler.ScaleAction{
					Rules: []scaler.ScaleRule{
						{Metric: scaler.MetricCPUPercent, Threshold: 80, Operator: scaler.OperatorGreaterThan},
					},
					Delta: 1,
				},
				ScaleDown:   scaler.ScaleAction{Delta: 1},
				MinReplicas: 1,
				MaxReplicas: 10,
			},
			wantErr: true,
		},
		{
			name: "invalid replicas",
			policy: &scaler.Policy{
				Name:   "invalid-replicas",
				Target: scaler.ScaleTarget{Type: scaler.TargetTypeTenant, ID: "tenant-1"},
				ScaleUp: scaler.ScaleAction{
					Rules: []scaler.ScaleRule{
						{Metric: scaler.MetricCPUPercent, Threshold: 80, Operator: scaler.OperatorGreaterThan},
					},
					Delta: 1,
				},
				ScaleDown:   scaler.ScaleAction{Delta: 1},
				MinReplicas: 10,
				MaxReplicas: 1, // Max < Min
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.policy.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestScaleRule(t *testing.T) {
	tests := []struct {
		name    string
		rule    scaler.ScaleRule
		metrics *scaler.TenantMetrics
		want    bool
	}{
		{
			name: "CPU greater than threshold",
			rule: scaler.ScaleRule{
				Metric:    scaler.MetricCPUPercent,
				Threshold: 80.0,
				Operator:  scaler.OperatorGreaterThan,
			},
			metrics: &scaler.TenantMetrics{
				AvgCPUPercent: 85.0,
			},
			want: true,
		},
		{
			name: "CPU less than threshold",
			rule: scaler.ScaleRule{
				Metric:    scaler.MetricCPUPercent,
				Threshold: 20.0,
				Operator:  scaler.OperatorLessThan,
			},
			metrics: &scaler.TenantMetrics{
				AvgCPUPercent: 15.0,
			},
			want: true,
		},
		{
			name: "Agent count greater or equal",
			rule: scaler.ScaleRule{
				Metric:    scaler.MetricAgentCount,
				Threshold: 5.0,
				Operator:  scaler.OperatorGreaterOrEqual,
			},
			metrics: &scaler.TenantMetrics{
				AgentCount: 5,
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.rule.Evaluate(tt.metrics)
			if got != tt.want {
				t.Errorf("Evaluate() = %v, want %v", got, tt.want)
			}
		})
	}
}
