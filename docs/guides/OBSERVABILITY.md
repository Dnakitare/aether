# Observability Guide

Complete guide to monitoring, tracing, and debugging Aether Runtime in production.

## Table of Contents

- [Overview](#overview)
- [Distributed Tracing](#distributed-tracing)
- [Metrics](#metrics)
- [Logging](#logging)
- [Dashboards](#dashboards)
- [Alerting](#alerting)
- [Performance Monitoring](#performance-monitoring)
- [Debugging](#debugging)

## Overview

Aether provides comprehensive observability through three pillars:

1. **Distributed Tracing** - OpenTelemetry with Jaeger
2. **Metrics** - Prometheus with Grafana dashboards
3. **Structured Logging** - JSON logs with trace context

## Distributed Tracing

### Overview

Aether uses OpenTelemetry for distributed tracing, allowing you to track requests across services.

### Setup

#### 1. Configure Tracing

```bash
# Environment variables
export AETHER_TRACING_ENABLED=true
export AETHER_TRACING_ENDPOINT=http://jaeger:4318
export AETHER_TRACING_SERVICE_NAME=aether
export AETHER_TRACING_SAMPLING_RATE=0.1  # 10% sampling
```

#### 2. Deploy Jaeger

**Docker Compose:**
```yaml
jaeger:
  image: jaegertracing/all-in-one:1.51
  ports:
    - "16686:16686"  # UI
    - "4317:4317"    # OTLP gRPC
    - "4318:4318"    # OTLP HTTP
  environment:
    COLLECTOR_OTLP_ENABLED: "true"
```

**Kubernetes:**
```bash
kubectl create namespace observability
kubectl apply -f https://raw.githubusercontent.com/jaegertracing/jaeger-operator/main/deploy/crds/jaegertracing.io_jaegers_crd.yaml
kubectl apply -f - <<EOF
apiVersion: jaegertracing.io/v1
kind: Jaeger
metadata:
  name: jaeger
  namespace: observability
spec:
  strategy: production
  storage:
    type: elasticsearch
EOF
```

### Using Traces

#### View Traces in Jaeger UI

1. Open Jaeger UI: `http://localhost:16686`
2. Select service: **aether**
3. Search for operations:
   - `CreateAgent`
   - `ScheduleAgent`
   - `HTTP GET /agents`

#### Trace Structure

```
HTTP Request (Trace ID: abc123)
├── API Handler: GET /agents
│   ├── Auth Middleware
│   ├── Runtime.ListAgents
│   │   ├── Database Query
│   │   └── Cache Lookup
│   └── Response Serialization
└── Total Duration: 45ms
```

#### Trace Context in Logs

Every log entry includes trace context:

```json
{
  "timestamp": "2026-02-15T10:30:00Z",
  "level": "info",
  "msg": "agent created",
  "trace_id": "abc123...",
  "span_id": "def456...",
  "agent_id": "agent-1"
}
```

### Key Traced Operations

| Operation | Description | Avg Duration |
|-----------|-------------|--------------|
| `CreateAgent` | Complete agent creation | 500ms-2s |
| `ScheduleAgent` | Agent scheduling | 10-50ms |
| `HTTP /agents` | List agents API | 20-100ms |
| `Database Query` | SQL operations | 1-20ms |
| `Cache Get` | Redis operations | 1-5ms |

### Custom Spans

Add custom spans in your code:

```go
import "go.opentelemetry.io/otel"

func ProcessData(ctx context.Context) error {
    tracer := otel.Tracer("aether")
    ctx, span := tracer.Start(ctx, "ProcessData")
    defer span.End()

    // Your code here
    span.SetAttributes(
        attribute.String("data.size", "1024"),
        attribute.Int("items.count", 42),
    )

    return nil
}
```

## Metrics

### Available Metrics

Aether exposes Prometheus metrics at `http://localhost:8080/metrics`.

#### System Metrics

```promql
# Request metrics
aether_http_requests_total{method="GET",path="/agents",status="200"}
aether_http_request_duration_seconds{method="GET",path="/agents"}
aether_http_requests_in_flight

# Agent metrics
aether_agents_total{status="running"}
aether_agents_created_total
aether_agents_failed_total
aether_agent_creation_duration_seconds

# Scheduler metrics
aether_scheduler_queue_depth
aether_scheduler_placement_duration_seconds
aether_scheduler_placements_total{result="success"}

# Database metrics
aether_db_connections_open
aether_db_connections_in_use
aether_db_query_duration_seconds{operation="select"}

# Cache metrics
aether_cache_hits_total
aether_cache_misses_total
aether_cache_operations_duration_seconds{operation="get"}
```

### Prometheus Configuration

```yaml
# prometheus.yml
global:
  scrape_interval: 15s
  evaluation_interval: 15s

scrape_configs:
  - job_name: 'aether'
    static_configs:
      - targets: ['aether:8080']
    metrics_path: '/metrics'
    scrape_interval: 10s
```

### Querying Metrics

#### Request Rate

```promql
# Requests per second
rate(aether_http_requests_total[5m])

# By status code
sum by (status) (rate(aether_http_requests_total[5m]))
```

#### Latency

```promql
# P95 latency
histogram_quantile(0.95,
  rate(aether_http_request_duration_seconds_bucket[5m])
)

# Average latency by path
avg by (path) (rate(aether_http_request_duration_seconds_sum[5m]))
```

#### Error Rate

```promql
# Error rate (5xx responses)
sum(rate(aether_http_requests_total{status=~"5.."}[5m])) /
sum(rate(aether_http_requests_total[5m]))
```

#### Agent Metrics

```promql
# Total active agents
sum(aether_agents_total{status="running"})

# Agent creation rate
rate(aether_agents_created_total[5m])

# Agent failure rate
rate(aether_agents_failed_total[5m]) /
rate(aether_agents_created_total[5m])
```

#### Resource Usage

```promql
# Memory usage
process_resident_memory_bytes

# CPU usage
rate(process_cpu_seconds_total[5m])

# Goroutines
go_goroutines
```

## Logging

### Log Format

Aether uses structured JSON logging:

```json
{
  "timestamp": "2026-02-15T10:30:00.123Z",
  "level": "info",
  "msg": "agent created successfully",
  "component": "runtime",
  "agent_id": "agent-123",
  "tenant_id": "tenant-1",
  "trace_id": "abc123...",
  "span_id": "def456...",
  "duration_ms": 234
}
```

### Log Levels

```bash
# Set log level
export AETHER_LOG_LEVEL=debug  # debug, info, warn, error

# Enable JSON format (production)
export AETHER_LOG_FORMAT=json

# Enable human-readable format (development)
export AETHER_LOG_FORMAT=text
```

### Log Aggregation

#### With Loki (Kubernetes)

```yaml
# Promtail config
scrape_configs:
  - job_name: kubernetes-pods
    kubernetes_sd_configs:
      - role: pod
    pipeline_stages:
      - json:
          expressions:
            level: level
            msg: msg
            trace_id: trace_id
      - labels:
          level:
          trace_id:
```

#### With ELK Stack

```yaml
# Filebeat config
filebeat.inputs:
  - type: container
    paths:
      - '/var/lib/docker/containers/*/*.log'
    json.keys_under_root: true
    json.add_error_key: true

output.elasticsearch:
  hosts: ["elasticsearch:9200"]
```

### Common Log Queries

#### Find Errors

```bash
# Using jq
kubectl logs -n aether deployment/aether-api | jq 'select(.level=="error")'

# Filter by trace ID
kubectl logs -n aether deployment/aether-api | jq 'select(.trace_id=="abc123")'
```

#### Correlate with Traces

```bash
# Get trace ID from logs
TRACE_ID=$(kubectl logs -n aether deployment/aether-api | \
  jq -r 'select(.msg=="agent created") | .trace_id' | head -1)

# View in Jaeger
open "http://localhost:16686/trace/$TRACE_ID"
```

## Dashboards

### Grafana Dashboards

Aether includes pre-built Grafana dashboards in `deployments/monitoring/grafana/dashboards/`:

1. **System Overview** (`system-overview.json`)
   - Request rate and latency
   - Error rates
   - Resource usage

2. **Agent Metrics** (`agent-metrics.json`)
   - Active agents
   - Creation/failure rates
   - Agent lifecycle events

3. **Scheduler Performance** (`scheduler-performance.json`)
   - Queue depth
   - Placement duration
   - Success/failure rates

4. **API Latency** (`api-latency.json`)
   - P50, P95, P99 latencies
   - Latency by endpoint
   - Slow requests

### Import Dashboards

```bash
# Using Grafana API
for dashboard in deployments/monitoring/grafana/dashboards/*.json; do
  curl -X POST http://admin:admin@localhost:3000/api/dashboards/db \
    -H "Content-Type: application/json" \
    -d @"$dashboard"
done
```

### Key Panels

#### Request Rate
```promql
sum(rate(aether_http_requests_total[5m])) by (method, path)
```

#### Error Rate
```promql
sum(rate(aether_http_requests_total{status=~"5.."}[5m])) /
sum(rate(aether_http_requests_total[5m])) * 100
```

#### Active Agents
```promql
sum(aether_agents_total{status="running"})
```

#### P95 Latency
```promql
histogram_quantile(0.95,
  sum by (le) (rate(aether_http_request_duration_seconds_bucket[5m]))
)
```

## Alerting

### Alert Rules

```yaml
# prometheus-alerts.yml
groups:
  - name: aether
    rules:
      # High error rate
      - alert: HighErrorRate
        expr: |
          sum(rate(aether_http_requests_total{status=~"5.."}[5m])) /
          sum(rate(aether_http_requests_total[5m])) > 0.05
        for: 5m
        labels:
          severity: critical
        annotations:
          summary: "High error rate (>5%)"
          description: "Error rate is {{ $value | humanizePercentage }}"

      # High latency
      - alert: HighLatency
        expr: |
          histogram_quantile(0.95,
            rate(aether_http_request_duration_seconds_bucket[5m])
          ) > 1
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "High API latency"
          description: "P95 latency is {{ $value }}s"

      # Agent creation failures
      - alert: HighAgentFailureRate
        expr: |
          rate(aether_agents_failed_total[5m]) /
          rate(aether_agents_created_total[5m]) > 0.1
        for: 10m
        labels:
          severity: warning
        annotations:
          summary: "High agent failure rate"

      # Database connection issues
      - alert: DatabaseConnectionPoolExhausted
        expr: |
          aether_db_connections_in_use / aether_db_connections_max > 0.9
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "Database connection pool nearly exhausted"

      # Scheduler queue backed up
      - alert: SchedulerQueueBacklog
        expr: aether_scheduler_queue_depth > 1000
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "Scheduler queue has {{ $value }} items"
```

### Alert Channels

```yaml
# Alertmanager config
route:
  group_by: ['alertname', 'severity']
  group_wait: 10s
  group_interval: 10s
  repeat_interval: 12h
  receiver: 'slack'

receivers:
  - name: 'slack'
    slack_configs:
      - api_url: 'https://hooks.slack.com/services/XXX'
        channel: '#alerts'
        title: '{{ .GroupLabels.alertname }}'
        text: '{{ range .Alerts }}{{ .Annotations.description }}{{ end }}'

  - name: 'pagerduty'
    pagerduty_configs:
      - service_key: 'XXX'
        severity: '{{ .GroupLabels.severity }}'
```

## Performance Monitoring

### Key Performance Indicators

| Metric | Target | Alert Threshold |
|--------|--------|-----------------|
| **API Latency (P95)** | <100ms | >500ms |
| **Error Rate** | <1% | >5% |
| **Agent Creation Time** | <2s | >10s |
| **Scheduler Placement** | <50ms | >500ms |
| **Database Query** | <20ms | >100ms |

### Performance Profiling

```bash
# CPU profile
curl http://localhost:8080/debug/pprof/profile?seconds=30 > cpu.prof
go tool pprof cpu.prof

# Memory profile
curl http://localhost:8080/debug/pprof/heap > heap.prof
go tool pprof heap.prof

# Goroutine profile
curl http://localhost:8080/debug/pprof/goroutine > goroutine.prof
go tool pprof goroutine.prof
```

## Debugging

### Debug Endpoints

```bash
# Health check
curl http://localhost:8080/health

# Readiness check
curl http://localhost:8080/ready

# Metrics
curl http://localhost:8080/metrics

# Profiling (if enabled)
curl http://localhost:8080/debug/pprof/
```

### Trace Debugging

```bash
# Find slow requests
# In Jaeger UI, filter by:
- Duration > 1s
- Tags: error=true
```

### Log Debugging

```bash
# Enable debug logging
export AETHER_LOG_LEVEL=debug

# View real-time logs
kubectl logs -f -n aether deployment/aether-api

# Search for specific agent
kubectl logs -n aether deployment/aether-api | \
  jq 'select(.agent_id=="agent-123")'
```

## Best Practices

1. **Always enable tracing in production** (with sampling)
2. **Set up alerts for critical metrics**
3. **Monitor P95/P99 latencies, not just averages**
4. **Correlate logs with traces using trace IDs**
5. **Review dashboards regularly**
6. **Test alert rules before deploying**
7. **Keep retention policies appropriate** (7-30 days)
8. **Use distributed tracing for debugging complex issues**

## Next Steps

- Set up [Alerting](./ALERTING.md)
- Review [Troubleshooting Guide](./TROUBLESHOOTING.md)
- Configure [Performance Tuning](./PERFORMANCE.md)

---

**Last Updated**: February 15, 2026
**Version**: 0.2.0-beta
