package observability

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/dnakitare/aether/pkg/api"
)

// MetricsCollector provides Prometheus metrics collection.
type MetricsCollector struct {
	logger *slog.Logger

	// API metrics
	apiRequestsTotal    *prometheus.CounterVec
	apiRequestDuration  *prometheus.HistogramVec
	apiRequestsInFlight *prometheus.GaugeVec

	// Agent metrics
	agentsTotal          *prometheus.GaugeVec
	agentOperationsTotal *prometheus.CounterVec
	agentStartupDuration *prometheus.HistogramVec
	agentErrors          *prometheus.CounterVec

	// Resource metrics
	cpuUsage    *prometheus.GaugeVec
	memoryUsage *prometheus.GaugeVec

	// Scheduler metrics
	schedulingDuration *prometheus.HistogramVec
	schedulingErrors   *prometheus.CounterVec

	// Cost metrics
	resourceCost *prometheus.CounterVec

	registry *prometheus.Registry
}

// NewMetricsCollector creates a new metrics collector.
func NewMetricsCollector(logger *slog.Logger) *MetricsCollector {
	registry := prometheus.NewRegistry()

	mc := &MetricsCollector{
		logger: logger.With("component", "metrics"),

		// API metrics
		apiRequestsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "aether_api_requests_total",
				Help: "Total number of API requests",
			},
			[]string{"method", "endpoint", "status", "tenant_id"},
		),
		apiRequestDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "aether_api_request_duration_seconds",
				Help:    "API request duration in seconds",
				Buckets: prometheus.DefBuckets,
			},
			[]string{"method", "endpoint", "tenant_id"},
		),
		apiRequestsInFlight: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "aether_api_requests_in_flight",
				Help: "Current number of API requests being processed",
			},
			[]string{"method", "endpoint"},
		),

		// Agent metrics
		agentsTotal: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "aether_agents_total",
				Help: "Total number of agents by status",
			},
			[]string{"status", "tenant_id"},
		),
		agentOperationsTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "aether_agent_operations_total",
				Help: "Total number of agent operations",
			},
			[]string{"operation", "status", "tenant_id"},
		),
		agentStartupDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "aether_agent_startup_duration_seconds",
				Help:    "Agent startup duration in seconds",
				Buckets: []float64{0.1, 0.5, 1, 2, 5, 10},
			},
			[]string{"tenant_id"},
		),
		agentErrors: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "aether_agent_errors_total",
				Help: "Total number of agent errors",
			},
			[]string{"error_type", "tenant_id"},
		),

		// Resource metrics
		cpuUsage: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "aether_cpu_usage_cores",
				Help: "CPU usage in cores",
			},
			[]string{"agent_id", "tenant_id"},
		),
		memoryUsage: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "aether_memory_usage_bytes",
				Help: "Memory usage in bytes",
			},
			[]string{"agent_id", "tenant_id"},
		),

		// Scheduler metrics
		schedulingDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "aether_scheduling_duration_seconds",
				Help:    "Scheduling operation duration in seconds",
				Buckets: []float64{0.01, 0.05, 0.1, 0.5, 1},
			},
			[]string{"strategy", "tenant_id"},
		),
		schedulingErrors: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "aether_scheduling_errors_total",
				Help: "Total number of scheduling errors",
			},
			[]string{"reason", "tenant_id"},
		),

		// Cost metrics
		resourceCost: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "aether_resource_cost_total",
				Help: "Total resource cost in dollars",
			},
			[]string{"resource_type", "tenant_id"},
		),

		registry: registry,
	}

	// Register all metrics
	registry.MustRegister(
		mc.apiRequestsTotal,
		mc.apiRequestDuration,
		mc.apiRequestsInFlight,
		mc.agentsTotal,
		mc.agentOperationsTotal,
		mc.agentStartupDuration,
		mc.agentErrors,
		mc.cpuUsage,
		mc.memoryUsage,
		mc.schedulingDuration,
		mc.schedulingErrors,
		mc.resourceCost,
	)

	logger.Info("metrics collector initialized")
	return mc
}

// Handler returns the HTTP handler for Prometheus metrics.
func (mc *MetricsCollector) Handler() http.Handler {
	return promhttp.HandlerFor(mc.registry, promhttp.HandlerOpts{})
}

// RecordAPIRequest records an API request.
func (mc *MetricsCollector) RecordAPIRequest(method, endpoint, status string, tenantID api.TenantID, duration time.Duration) {
	mc.apiRequestsTotal.WithLabelValues(method, endpoint, status, string(tenantID)).Inc()
	mc.apiRequestDuration.WithLabelValues(method, endpoint, string(tenantID)).Observe(duration.Seconds())
}

// IncAPIRequestsInFlight increments in-flight API requests.
func (mc *MetricsCollector) IncAPIRequestsInFlight(method, endpoint string) {
	mc.apiRequestsInFlight.WithLabelValues(method, endpoint).Inc()
}

// DecAPIRequestsInFlight decrements in-flight API requests.
func (mc *MetricsCollector) DecAPIRequestsInFlight(method, endpoint string) {
	mc.apiRequestsInFlight.WithLabelValues(method, endpoint).Dec()
}

// SetAgentCount sets the total agent count for a given status.
func (mc *MetricsCollector) SetAgentCount(status api.AgentStatus, tenantID api.TenantID, count float64) {
	mc.agentsTotal.WithLabelValues(string(status), string(tenantID)).Set(count)
}

// RecordAgentOperation records an agent operation.
func (mc *MetricsCollector) RecordAgentOperation(operation string, status string, tenantID api.TenantID) {
	mc.agentOperationsTotal.WithLabelValues(operation, status, string(tenantID)).Inc()
}

// RecordAgentStartup records agent startup duration.
func (mc *MetricsCollector) RecordAgentStartup(tenantID api.TenantID, duration time.Duration) {
	mc.agentStartupDuration.WithLabelValues(string(tenantID)).Observe(duration.Seconds())
}

// RecordAgentError records an agent error.
func (mc *MetricsCollector) RecordAgentError(errorType string, tenantID api.TenantID) {
	mc.agentErrors.WithLabelValues(errorType, string(tenantID)).Inc()
}

// SetCPUUsage sets CPU usage for an agent.
func (mc *MetricsCollector) SetCPUUsage(agentID api.AgentID, tenantID api.TenantID, cores float64) {
	mc.cpuUsage.WithLabelValues(string(agentID), string(tenantID)).Set(cores)
}

// SetMemoryUsage sets memory usage for an agent.
func (mc *MetricsCollector) SetMemoryUsage(agentID api.AgentID, tenantID api.TenantID, bytes float64) {
	mc.memoryUsage.WithLabelValues(string(agentID), string(tenantID)).Set(bytes)
}

// RecordSchedulingDuration records scheduling operation duration.
func (mc *MetricsCollector) RecordSchedulingDuration(strategy string, tenantID api.TenantID, duration time.Duration) {
	mc.schedulingDuration.WithLabelValues(strategy, string(tenantID)).Observe(duration.Seconds())
}

// RecordSchedulingError records a scheduling error.
func (mc *MetricsCollector) RecordSchedulingError(reason string, tenantID api.TenantID) {
	mc.schedulingErrors.WithLabelValues(reason, string(tenantID)).Inc()
}

// RecordResourceCost records resource cost.
func (mc *MetricsCollector) RecordResourceCost(resourceType string, tenantID api.TenantID, cost float64) {
	mc.resourceCost.WithLabelValues(resourceType, string(tenantID)).Add(cost)
}

// StartMetricsServer starts the metrics HTTP server.
func (mc *MetricsCollector) StartMetricsServer(ctx context.Context, addr string) error {
	mux := http.NewServeMux()
	mux.Handle("/metrics", mc.Handler())

	server := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	// Graceful shutdown
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			mc.logger.ErrorContext(ctx, "metrics server shutdown error", "error", err)
		}
	}()

	mc.logger.Info("starting metrics server", "addr", addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("metrics server error: %w", err)
	}

	return nil
}
