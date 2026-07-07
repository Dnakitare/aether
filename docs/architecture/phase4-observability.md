# Phase 4: Observability - Architecture

## Overview

Phase 4 implements comprehensive observability for Aether, including distributed tracing, metrics collection, centralized logging, cost tracking, and behavioral monitoring. This phase provides complete visibility into system behavior, performance, costs, and security.

## New Components

### 1. Distributed Tracing (`internal/observability/tracing.go`)

OpenTelemetry-based distributed tracing for end-to-end request visibility.

#### Key Features:
- **OpenTelemetry SDK**: Industry-standard tracing
- **Multiple exporters**: OTLP gRPC, Jaeger, stdout
- **W3C trace context**: Standard propagation format
- **Configurable sampling**: Control trace volume
- **Trace correlation**: Automatic log correlation with trace/span IDs
- **Resource attributes**: Service metadata in all traces

#### Architecture:
```
Request → TracerProvider → Span → Exporter → Backend (Jaeger/OTLP)
                            ↓
                        Context propagation
                            ↓
                        Logs (with trace_id)
```

#### Configuration:
```go
TracerConfig{
    Enabled:       true,
    Endpoint:      "localhost:4317",  // OTLP gRPC endpoint
    SamplingRatio: 1.0,                // Sample all traces (0.0-1.0)
    Environment:   "production",
    UseStdout:     false,              // Use for development
}
```

#### Usage Example:
```go
tracer := tracerProvider.Tracer("aether-api")
ctx, span := tracer.Start(ctx, "create-agent")
defer span.End()

// Add attributes
span.SetAttributes(
    attribute.String("tenant_id", tenantID),
    attribute.String("agent_id", agentID),
)

// Record events
span.AddEvent("agent-created", trace.WithAttributes(
    attribute.String("status", "success"),
))

// Record errors
if err != nil {
    span.RecordError(err)
    span.SetStatus(codes.Error, err.Error())
}
```

### 2. Prometheus Metrics (`internal/observability/metrics.go`)

Comprehensive metrics collection for monitoring and alerting.

#### Metric Types:

**API Metrics:**
- `aether_api_requests_total`: Counter of API requests by method, endpoint, status, tenant
- `aether_api_request_duration_seconds`: Histogram of request latency
- `aether_api_requests_in_flight`: Gauge of concurrent requests

**Agent Metrics:**
- `aether_agents_total`: Gauge of agents by status and tenant
- `aether_agent_operations_total`: Counter of agent operations (create, start, stop)
- `aether_agent_startup_duration_seconds`: Histogram of startup times
- `aether_agent_errors_total`: Counter of agent errors by type

**Resource Metrics:**
- `aether_cpu_usage_cores`: Gauge of CPU usage per agent
- `aether_memory_usage_bytes`: Gauge of memory usage per agent

**Scheduler Metrics:**
- `aether_scheduling_duration_seconds`: Histogram of scheduling latency
- `aether_scheduling_errors_total`: Counter of scheduling errors

**Cost Metrics:**
- `aether_resource_cost_total`: Counter of resource costs by type and tenant

#### Usage Example:
```go
// Record API request
metrics.RecordAPIRequest("POST", "/v1/agents", "200", tenantID, 150*time.Millisecond)

// Track in-flight requests
metrics.IncAPIRequestsInFlight("POST", "/v1/agents")
defer metrics.DecAPIRequestsInFlight("POST", "/v1/agents")

// Update agent count
metrics.SetAgentCount(api.AgentStatusRunning, tenantID, 10)

// Record agent startup
startTime := time.Now()
// ... agent creation ...
metrics.RecordAgentStartup(tenantID, time.Since(startTime))
```

### 3. Cost Tracking (`internal/observability/cost.go`)

Track resource usage and calculate costs per tenant.

#### Key Features:
- **Resource tracking**: CPU core-hours, memory GB-hours, storage, network
- **Configurable pricing**: Customizable per-resource pricing
- **Cost aggregation**: Daily, weekly, monthly summaries
- **Anomaly detection**: Detect unexpected cost spikes
- **PostgreSQL storage**: Durable cost records
- **Retention policies**: Automatic cleanup of old records

#### Pricing Configuration:
```go
PricingConfig{
    CPUCoreHour:    0.04,  // $0.04 per core-hour
    MemoryGBHour:   0.005, // $0.005 per GB-hour
    StorageGBMonth: 0.10,  // $0.10 per GB-month
    NetworkGB:      0.09,  // $0.09 per GB transferred
}
```

#### Database Schema:
```sql
CREATE TABLE costs (
    id BIGSERIAL PRIMARY KEY,
    timestamp TIMESTAMP WITH TIME ZONE NOT NULL,
    tenant_id VARCHAR(255) NOT NULL,
    agent_id VARCHAR(255),
    resource_type VARCHAR(50) NOT NULL,  -- cpu, memory, storage, network
    amount DOUBLE PRECISION NOT NULL,     -- resource amount
    cost DOUBLE PRECISION NOT NULL,       -- cost in dollars
    period_seconds BIGINT NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);
```

#### Usage Example:
```go
// Record CPU usage
costTracker.RecordCPUUsage(ctx, tenantID, agentID, 2.0, 1*time.Hour)
// Cost: 2 cores * 1 hour * $0.04 = $0.08

// Record memory usage
costTracker.RecordMemoryUsage(ctx, tenantID, agentID, 4.0, 1*time.Hour)
// Cost: 4 GB * 1 hour * $0.005 = $0.02

// Get cost summary
summary, _ := costTracker.GetCostSummary(ctx, tenantID, startTime, endTime)
fmt.Printf("Total cost: $%.2f\n", summary.TotalCost)
fmt.Printf("CPU: $%.2f, Memory: $%.2f\n", summary.CPUCost, summary.MemoryCost)

// Detect anomalies (costs > 3 std deviations from average)
anomalies, _ := costTracker.DetectCostAnomalies(ctx, tenantID, 3.0)
for _, anomaly := range anomalies {
    fmt.Printf("Cost spike at %s: $%.2f (avg: $%.2f, %.1fx deviation)\n",
        anomaly.Timestamp, anomaly.Cost, anomaly.AverageCost, anomaly.DeviationFactor)
}
```

### 4. Behavioral Monitoring (`internal/observability/behavior.go`)

Security-focused behavioral monitoring and anomaly detection.

#### Key Features:
- **Pattern tracking**: Agent spawning, API calls, resource usage
- **Baseline learning**: Establish normal behavior over time
- **Anomaly detection**: Statistical deviation from baseline
- **Automated alerts**: Configurable alert callbacks
- **Response actions**: Throttle, revoke, terminate, escalate
- **Severity levels**: Low, medium, high, critical

#### Monitored Behaviors:
- **Agent spawning**: Excessive agent creation
- **API calls**: Abnormal API request rates
- **CPU usage**: Unexpected CPU spikes
- **Memory usage**: Memory consumption anomalies

#### Configuration:
```go
MonitorConfig{
    WindowSize:       1 * time.Hour,      // Tracking window
    BaselineSize:     24,                  // Hours for baseline
    AnomalyThreshold: 3.0,                 // Std deviations for anomaly
    AlertCooldown:    15 * time.Minute,    // Alert rate limiting
}
```

#### Anomaly Response Actions:
```go
const (
    ActionNone      ResponseAction = "none"        // Log only
    ActionThrottle  ResponseAction = "throttle"     // Rate limit
    ActionRevoke    ResponseAction = "revoke_permissions"  // Remove perms
    ActionTerminate ResponseAction = "terminate"    // Kill agent
    ActionEscalate  ResponseAction = "escalate"     // Human review
)
```

#### Usage Example:
```go
// Create monitor with alert callback
monitor := observability.NewBehaviorMonitor(logger, config, func(ctx context.Context, anomaly *Anomaly) error {
    logger.WarnContext(ctx, "anomaly detected",
        "type", anomaly.Type,
        "severity", anomaly.Severity,
        "action", anomaly.Action,
        "description", anomaly.Description,
    )

    // Take automated action
    switch anomaly.Action {
    case ActionThrottle:
        return throttleAgent(anomaly.AgentID)
    case ActionTerminate:
        return terminateAgent(anomaly.AgentID)
    case ActionEscalate:
        return sendAlert(anomaly)
    }
    return nil
})

// Start monitoring loop
go monitor.StartMonitoring(ctx)

// Record agent behavior
monitor.RecordAgentSpawn(ctx, tenantID, agentID, parentID)
monitor.RecordAPICall(ctx, tenantID, agentID)
monitor.RecordResourceUsage(ctx, tenantID, agentID, cpuCores, memoryGB)
```

## Observability Stack Deployment

### Docker Compose Setup (`deployments/observability.yaml`)

Complete observability stack for local development:

```yaml
services:
  jaeger:       # Distributed tracing
  prometheus:   # Metrics collection
  grafana:      # Visualization
  loki:         # Log aggregation
  promtail:     # Log shipping
  postgres:     # Audit logs & cost tracking
  redis:        # State persistence
```

### Starting the Stack:
```bash
cd deployments
docker-compose -f observability.yaml up -d

# Access services:
# - Jaeger UI: http://localhost:16686
# - Prometheus: http://localhost:9090
# - Grafana: http://localhost:3000 (admin/admin)
# - Loki: http://localhost:3100
```

### Prometheus Configuration

Scrapes metrics from Aether runtime every 15 seconds:

```yaml
scrape_configs:
  - job_name: 'aether'
    static_configs:
      - targets: ['host.docker.internal:9091']
```

### Alert Rules

Pre-configured alerts for common issues:

- **HighAgentErrorRate**: Agent error rate > 0.1/sec
- **AgentStartupSlow**: P95 startup > 10s
- **HighAPILatency**: P95 latency > 1s
- **HighAPIErrorRate**: 5xx rate > 5%
- **HighCPUUsage**: Tenant using > 80 cores
- **UnexpectedCostSpike**: Cost > $10/hour

## Grafana Dashboards

### Aether Overview Dashboard

Key metrics for system health:

**Panels:**
1. Total Agents (stat)
2. API Requests (stat, 5m rate)
3. Total CPU Usage (stat, cores)
4. Total Memory (stat, GB)
5. Agents by Status (timeseries)
6. API Request Rate by Endpoint (timeseries)
7. API Latency P50/P95/P99 (timeseries)
8. Agent Errors (timeseries)
9. CPU Usage by Tenant (timeseries)
10. Resource Cost by Tenant (timeseries, $/hr)

### Accessing Dashboards:
```bash
# Open Grafana
open http://localhost:3000

# Navigate to: Dashboards → Aether → Aether Overview
```

## Integration with Existing Components

### API Server Integration

```go
// Add metrics middleware
router.Use(func(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        start := time.Now()
        metrics.IncAPIRequestsInFlight(r.Method, r.URL.Path)
        defer metrics.DecAPIRequestsInFlight(r.Method, r.URL.Path)

        // Wrap response writer to capture status
        wrapped := &responseWriter{ResponseWriter: w, statusCode: 200}
        next.ServeHTTP(wrapped, r)

        duration := time.Since(start)
        metrics.RecordAPIRequest(
            r.Method,
            r.URL.Path,
            fmt.Sprintf("%d", wrapped.statusCode),
            getTenantID(r),
            duration,
        )
    })
})

// Expose /metrics endpoint
http.Handle("/metrics", metrics.Handler())
```

### Runtime Integration

```go
// Initialize observability
tracerProvider, _ := observability.NewTracerProvider(logger, tracerConfig)
defer tracerProvider.Shutdown(ctx)

metrics := observability.NewMetricsCollector(logger)
go metrics.StartMetricsServer(ctx, ":9091")

// Start cost tracking
costTracker, _ := observability.NewCostTracker(logger, db, pricing, metrics)

// Start behavioral monitoring
monitor := observability.NewBehaviorMonitor(logger, monitorConfig, alertCallback)
go monitor.StartMonitoring(ctx)

// Use in agent lifecycle
tracer := tracerProvider.Tracer("aether-runtime")
ctx, span := tracer.Start(ctx, "agent-create")
defer span.End()

startTime := time.Now()
agent, err := runtime.CreateAgent(ctx, config)
if err != nil {
    span.RecordError(err)
    metrics.RecordAgentError("create_failed", config.TenantID)
    return err
}

// Record metrics
metrics.RecordAgentStartup(config.TenantID, time.Since(startTime))
metrics.SetAgentCount(api.AgentStatusRunning, config.TenantID, agentCount)

// Track costs
go costTracker.RecordCPUUsage(ctx, config.TenantID, agent.ID, config.CPUCount, time.Hour)
go costTracker.RecordMemoryUsage(ctx, config.TenantID, agent.ID, config.MemoryGB, time.Hour)

// Monitor behavior
monitor.RecordAgentSpawn(ctx, config.TenantID, agent.ID, parentID)
```

## Performance Characteristics

### Distributed Tracing
- Span creation: ~10μs
- Context propagation: ~5μs
- Export batch: ~50ms (async)
- Sampling overhead: <1% with 100% sampling

### Metrics Collection
- Counter increment: ~100ns
- Gauge set: ~100ns
- Histogram observe: ~500ns
- Scrape duration: ~50ms

### Cost Tracking
- Cost record insert: ~2ms (PostgreSQL)
- Cost summary query: ~50ms (with indexes)
- Anomaly detection: ~100ms (24h window)

### Behavioral Monitoring
- Behavior record: ~50μs (in-memory)
- Baseline update: ~1ms
- Anomaly check: ~5ms

## Testing

### Test Coverage
- Distributed tracing: Unit tests with stdout exporter
- Metrics collection: Unit tests for all metric types
- Cost tracking: Unit tests for pricing, integration tests for DB
- Behavioral monitoring: Unit tests for anomaly detection

### Running Tests
```bash
# Unit tests
go test ./internal/observability/... -v

# Integration tests (requires PostgreSQL, Redis)
go test ./internal/observability/... -v -tags=integration

# Load test observability overhead
go test ./internal/observability/... -bench=. -benchmem
```

## Best Practices

### Tracing
```go
// ✓ DO: Create spans for significant operations
ctx, span := tracer.Start(ctx, "operation-name")
defer span.End()

// ✓ DO: Add meaningful attributes
span.SetAttributes(
    attribute.String("tenant_id", tenantID),
    attribute.Int("agent_count", count),
)

// ✓ DO: Record errors
if err != nil {
    span.RecordError(err)
    span.SetStatus(codes.Error, err.Error())
}

// ✗ DON'T: Create spans for trivial operations
// ✗ DON'T: Add sensitive data to span attributes
```

### Metrics
```go
// ✓ DO: Use appropriate metric types
counters      // Monotonically increasing (requests, errors)
gauges        // Can go up/down (active connections, memory)
histograms    // Distribution (latency, size)

// ✓ DO: Include important labels
metrics.RecordAPIRequest("POST", "/v1/agents", "200", tenantID, duration)

// ✗ DON'T: Use high-cardinality labels (user IDs, UUIDs)
// ✗ DON'T: Create too many metric types (memory overhead)
```

### Cost Tracking
```go
// ✓ DO: Record costs periodically (hourly is good)
ticker := time.NewTicker(1 * time.Hour)
for range ticker.C {
    costTracker.RecordCPUUsage(ctx, tenantID, agentID, cpuCores, 1*time.Hour)
}

// ✓ DO: Set appropriate retention
costTracker.CleanupOldRecords(ctx, 90) // 90 days

// ✗ DON'T: Record costs too frequently (creates noise)
// ✗ DON'T: Ignore cost anomalies (potential security issue)
```

### Behavioral Monitoring
```go
// ✓ DO: Set appropriate thresholds for your workload
config.AnomalyThreshold = 3.0  // 3 standard deviations

// ✓ DO: Implement graduated response
switch anomaly.Severity {
case SeverityLow:
    log.Warn("anomaly detected")
case SeverityHigh:
    throttleAgent(anomaly.AgentID)
case SeverityCritical:
    terminateAgent(anomaly.AgentID)
}

// ✗ DON'T: Use overly sensitive thresholds (false positives)
// ✗ DON'T: Ignore anomalies (defeats the purpose)
```

## Known Limitations

Phase 4 limitations (to be addressed in future phases):

- **Log aggregation**: Loki configuration is basic, production needs proper retention and compaction
- **Alert manager**: Alerts configured but not routed to notification channels
- **Dashboard automation**: Dashboards are static, no auto-generation from metrics
- **Trace sampling**: Simple ratio-based, no adaptive sampling
- **Cost allocation**: No support for spot instances or reserved capacity pricing
- **Behavioral monitoring**: Simple statistical anomaly detection, no ML-based prediction

## Next Steps (Phase 5)

Phase 5 will add:
- **Rate limiting**: Token bucket algorithm with multi-tier enforcement
- **Agent recovery**: Checkpointing and automatic restart
- **State management**: Enhanced Redis patterns for distributed operation

## Metrics Reference

### Complete Metric List

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `aether_api_requests_total` | Counter | method, endpoint, status, tenant_id | Total API requests |
| `aether_api_request_duration_seconds` | Histogram | method, endpoint, tenant_id | API request duration |
| `aether_api_requests_in_flight` | Gauge | method, endpoint | Current concurrent requests |
| `aether_agents_total` | Gauge | status, tenant_id | Agents by status |
| `aether_agent_operations_total` | Counter | operation, status, tenant_id | Agent operations |
| `aether_agent_startup_duration_seconds` | Histogram | tenant_id | Agent startup time |
| `aether_agent_errors_total` | Counter | error_type, tenant_id | Agent errors |
| `aether_cpu_usage_cores` | Gauge | agent_id, tenant_id | CPU usage in cores |
| `aether_memory_usage_bytes` | Gauge | agent_id, tenant_id | Memory usage in bytes |
| `aether_scheduling_duration_seconds` | Histogram | strategy, tenant_id | Scheduling latency |
| `aether_scheduling_errors_total` | Counter | reason, tenant_id | Scheduling errors |
| `aether_resource_cost_total` | Counter | resource_type, tenant_id | Resource costs |
