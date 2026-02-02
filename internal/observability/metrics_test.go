package observability_test

import (
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/aether-runtime/aether/internal/observability"
	"github.com/aether-runtime/aether/pkg/api"
)

func TestMetricsCollector(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	mc := observability.NewMetricsCollector(logger)

	// Test API request recording
	mc.RecordAPIRequest("POST", "/v1/agents", "200", api.TenantID("tenant-1"), 100*time.Millisecond)
	mc.RecordAPIRequest("GET", "/v1/agents", "200", api.TenantID("tenant-1"), 50*time.Millisecond)

	// Test in-flight requests
	mc.IncAPIRequestsInFlight("POST", "/v1/agents")
	mc.DecAPIRequestsInFlight("POST", "/v1/agents")

	// Test agent metrics
	mc.SetAgentCount(api.AgentStatusRunning, api.TenantID("tenant-1"), 5)
	mc.RecordAgentOperation("create", "success", api.TenantID("tenant-1"))
	mc.RecordAgentStartup(api.TenantID("tenant-1"), 2*time.Second)
	mc.RecordAgentError("timeout", api.TenantID("tenant-1"))

	// Test resource metrics
	mc.SetCPUUsage(api.AgentID("agent-1"), api.TenantID("tenant-1"), 2.5)
	mc.SetMemoryUsage(api.AgentID("agent-1"), api.TenantID("tenant-1"), 1024*1024*1024)

	// Test scheduler metrics
	mc.RecordSchedulingDuration("bin-packing", api.TenantID("tenant-1"), 50*time.Millisecond)
	mc.RecordSchedulingError("no_capacity", api.TenantID("tenant-1"))

	// Test cost metrics
	mc.RecordResourceCost("cpu", api.TenantID("tenant-1"), 0.05)
	mc.RecordResourceCost("memory", api.TenantID("tenant-1"), 0.02)

	// Verify handler is available
	if mc.Handler() == nil {
		t.Error("Expected metrics handler to be available")
	}
}
