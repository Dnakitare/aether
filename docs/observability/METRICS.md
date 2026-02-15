# Prometheus Metrics & Grafana Dashboards

Aether exposes comprehensive Prometheus metrics for monitoring system health, performance, and resource usage.

## Quick Start

### 1. Start the Observability Stack

```bash
cd deployments/docker
docker-compose -f docker-compose.dev.yml up -d

# Verify services are running
docker-compose -f docker-compose.dev.yml ps
```

**Services:**
- **Prometheus**: http://localhost:9090
- **Grafana**: http://localhost:3000 (admin/admin)
- **Jaeger**: http://localhost:16686

### 2. View Metrics

**Raw metrics endpoint:**
```bash
curl http://localhost:8080/metrics
```

**Prometheus UI:**
- Open http://localhost:9090
- Try queries like: `aether_agents_total`, `rate(aether_api_requests_total[5m])`

**Grafana Dashboards:**
- Open http://localhost:3000
- Login: admin/admin
- Navigate to Dashboards → Aether folder

## Available Dashboards

### 1. System Overview
**Path**: Aether → System Overview

**What it shows:**
- API request rate and latency
- Active agents by status
- Scheduling success rate
- Agent operations
- Scheduling and startup duration

**Use for:** Quick health check, overall system performance

### 2. Agent Metrics
**Path**: Aether → Agent Metrics

**What it shows:**
- Agents by status and tenant
- Agent operations rate
- Agent errors
- Startup duration heatmap
- CPU and memory usage per agent

**Use for:** Agent lifecycle monitoring, resource tracking

### 3. Scheduler Performance
**Path**: Aether → Scheduler Performance

**What it shows:**
- Scheduling duration percentiles (p50, p95, p99)
- Error rate and reasons
- Success vs errors
- Duration heatmap
- Scheduling by strategy

**Use for:** Optimizing placement algorithms, debugging slow scheduling

### 4. API Performance
**Path**: Aether → API Performance

**What it shows:**
- Request rate by endpoint
- Response time percentiles
- Status code distribution
- In-flight requests
- Error rate (4xx + 5xx)
- Request duration heatmap
- Requests by tenant

**Use for:** API health, SLA monitoring, capacity planning

## Metrics Reference

### API Metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `aether_api_requests_total` | Counter | method, endpoint, status, tenant_id | Total API requests |
| `aether_api_request_duration_seconds` | Histogram | method, endpoint, tenant_id | Request duration |
| `aether_api_requests_in_flight` | Gauge | method, endpoint | Current in-flight requests |

**Example queries:**
```promql
# Request rate
rate(aether_api_requests_total[5m])

# p95 latency
histogram_quantile(0.95, rate(aether_api_request_duration_seconds_bucket[5m]))

# Error rate
rate(aether_api_requests_total{status=~"5.."}[5m])
```

### Agent Metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `aether_agents_total` | Gauge | status, tenant_id | Total agents by status |
| `aether_agent_operations_total` | Counter | operation, status, tenant_id | Agent operations (create, start, stop) |
| `aether_agent_startup_duration_seconds` | Histogram | tenant_id | Agent startup time |
| `aether_agent_errors_total` | Counter | error_type, tenant_id | Agent errors |
| `aether_cpu_usage_cores` | Gauge | agent_id, tenant_id | CPU usage per agent |
| `aether_memory_usage_bytes` | Gauge | agent_id, tenant_id | Memory usage per agent |

**Example queries:**
```promql
# Agents by status
sum by(status) (aether_agents_total)

# Agent creation rate
rate(aether_agent_operations_total{operation="create"}[5m])

# p95 startup time
histogram_quantile(0.95, rate(aether_agent_startup_duration_seconds_bucket[5m]))
```

### Scheduler Metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `aether_scheduling_duration_seconds` | Histogram | strategy, tenant_id | Scheduling operation duration |
| `aether_scheduling_errors_total` | Counter | reason, tenant_id | Scheduling errors |

**Error reasons:**
- `no_suitable_node`: No nodes with enough capacity
- `node_capacity_changed`: Node capacity changed during placement

**Example queries:**
```promql
# Scheduling latency
histogram_quantile(0.99, rate(aether_scheduling_duration_seconds_bucket[5m]))

# Error rate by reason
sum by(reason) (rate(aether_scheduling_errors_total[5m]))
```

### Resource Metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `aether_resource_cost_total` | Counter | resource_type, tenant_id | Resource cost tracking |

## Alerts (Production)

**Recommended alert rules** (add to `prometheus.yml`):

```yaml
groups:
  - name: aether
    rules:
      # API latency too high
      - alert: HighAPILatency
        expr: histogram_quantile(0.95, rate(aether_api_request_duration_seconds_bucket[5m])) > 0.1
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "API latency p95 > 100ms"

      # High error rate
      - alert: HighErrorRate
        expr: rate(aether_api_requests_total{status=~"5.."}[5m]) > 0.05
        for: 5m
        labels:
          severity: critical
        annotations:
          summary: "API error rate > 5%"

      # Scheduling failures
      - alert: SchedulingFailures
        expr: rate(aether_scheduling_errors_total[5m]) > 0.1
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "High scheduling failure rate"

      # Agent creation failures
      - alert: AgentCreationFailures
        expr: rate(aether_agent_operations_total{operation="create",status="failed"}[5m]) > 0.05
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "High agent creation failure rate"
```

## Custom Dashboards

### Creating New Dashboards

1. **In Grafana UI:**
   - Create dashboard
   - Add panels
   - Export as JSON

2. **Save to git:**
   ```bash
   # Save to deployments/grafana/dashboards/
   cp ~/Downloads/my-dashboard.json deployments/grafana/dashboards/
   git add deployments/grafana/dashboards/my-dashboard.json
   git commit -m "Add custom dashboard"
   ```

3. **Auto-reload:**
   Grafana will automatically detect and load new dashboards.

### Useful Panel Types

- **Graph**: Time series data (request rate, latency)
- **Stat**: Single value (agent count, success rate)
- **Gauge**: Value with thresholds (success %, resource usage)
- **Heatmap**: Distribution over time (latency, duration)
- **Table**: Tabular data (top tenants, endpoints)
- **Pie Chart**: Proportions (status codes, error types)

## Retention & Storage

**Prometheus:**
- Default retention: 15 days
- Change in `docker-compose.dev.yml`:
  ```yaml
  command:
    - '--storage.tsdb.retention.time=30d'
  ```

**Grafana:**
- Dashboards stored in: `deployments/grafana/dashboards/`
- Data source config: `deployments/grafana/datasources/`
- User data: Docker volume `grafana-data`

## Troubleshooting

### No Metrics Showing

1. **Check Prometheus targets:**
   - Open http://localhost:9090/targets
   - Ensure `aether-api` target is UP
   - Check endpoint: `host.docker.internal:8080`

2. **Verify metrics endpoint:**
   ```bash
   curl http://localhost:8080/metrics | grep aether_
   ```

3. **Check Aether logs:**
   ```bash
   # Look for "metrics collector initialized"
   docker logs aether-api 2>&1 | grep metrics
   ```

### Grafana Can't Connect to Prometheus

1. **Check datasource config:**
   - Grafana → Configuration → Data Sources
   - URL should be: `http://prometheus:9090`
   - Test connection

2. **Check Docker network:**
   ```bash
   docker network inspect docker_default
   # Both prometheus and grafana should be in same network
   ```

### High Cardinality Issues

**Problem:** Too many unique label combinations

**Solution:**
- Avoid high-cardinality labels (agent_id in production)
- Use aggregation in queries
- Set up recording rules for common queries

```yaml
# prometheus.yml
rule_files:
  - 'recording_rules.yml'

# recording_rules.yml
groups:
  - name: aether_aggregates
    interval: 30s
    rules:
      - record: aether:api_requests:rate5m
        expr: sum by(endpoint) (rate(aether_api_requests_total[5m]))
```

## Best Practices

1. **Use rate() for counters:**
   ```promql
   rate(aether_api_requests_total[5m])  # Good
   aether_api_requests_total             # Bad (raw counter)
   ```

2. **Use histogram_quantile() for latency:**
   ```promql
   histogram_quantile(0.95, rate(aether_api_request_duration_seconds_bucket[5m]))
   ```

3. **Aggregate high-cardinality labels:**
   ```promql
   sum by(endpoint) (rate(aether_api_requests_total[5m]))  # Good
   rate(aether_api_requests_total[5m])                      # Bad (too many series)
   ```

4. **Set appropriate scrape intervals:**
   - Development: 15s
   - Production: 30s-60s

5. **Use recording rules for expensive queries:**
   - Pre-compute common aggregations
   - Reduce dashboard load time

## Production Deployment

### Prometheus

**Recommended setup:**
- Persistent storage (not Docker volumes)
- Remote write to long-term storage (Thanos, Cortex, Mimir)
- High availability (multiple Prometheus instances)
- Alertmanager for notifications

**Example remote write:**
```yaml
# prometheus.yml
remote_write:
  - url: "https://prometheus-remote-write.example.com/api/v1/write"
    basic_auth:
      username: "user"
      password: "pass"
```

### Grafana

**Recommended setup:**
- External database (PostgreSQL)
- OAuth/SAML authentication
- Read-only dashboards for viewers
- Backup dashboard JSON files

**Example with PostgreSQL:**
```yaml
# docker-compose.prod.yml
grafana:
  environment:
    - GF_DATABASE_TYPE=postgres
    - GF_DATABASE_HOST=postgres:5432
    - GF_DATABASE_NAME=grafana
    - GF_DATABASE_USER=grafana
    - GF_DATABASE_PASSWORD=${GRAFANA_DB_PASSWORD}
```

## Related Documentation

- [Distributed Tracing](TRACING.md)
- [Prometheus Docs](https://prometheus.io/docs/)
- [Grafana Docs](https://grafana.com/docs/)
- [PromQL Reference](https://prometheus.io/docs/prometheus/latest/querying/basics/)
