# Distributed Scheduler Testing Guide

This guide covers testing the distributed scheduler implementation with real infrastructure.

## Prerequisites

### Docker Desktop
Ensure Docker Desktop is running with sufficient resources:
- **Memory**: 8GB minimum, 16GB recommended
- **CPUs**: 4 cores minimum, 8 recommended
- **Disk**: 20GB free space

### Infrastructure Setup

Start the distributed infrastructure stack:

```bash
cd deployments/docker
docker-compose -f docker-compose.distributed.yaml up -d
```

This starts:
- PostgreSQL (port 5432)
- Redis (port 6379)
- Zookeeper (port 2181)
- Kafka (port 9092)
- etcd (port 2379)
- Kafka UI (port 8090) - optional, for debugging
- Redis Commander (port 8091) - optional, for debugging

### Verify Infrastructure

```bash
# Check all services are healthy
docker-compose -f docker-compose.distributed.yaml ps

# Should show all services as "healthy" or "running"
```

## Running Tests

### Quick Test (Integration Tests Only)

```bash
# Set environment variable to enable distributed tests
export DISTRIBUTED_INFRA_AVAILABLE=true

# Run integration tests
go test -v ./tests/integration/... -run TestDistributedScheduler
```

### Full Test Suite

```bash
# Run all distributed scheduler tests
export DISTRIBUTED_INFRA_AVAILABLE=true
go test -v ./tests/integration/... -timeout 10m
```

### Individual Test Scenarios

**End-to-End Flow:**
```bash
go test -v ./tests/integration/... -run TestDistributedScheduler_EndToEnd
```

**Multi-Scheduler Coordination:**
```bash
go test -v ./tests/integration/... -run TestDistributedScheduler_MultiScheduler
```

**Failover Scenario:**
```bash
go test -v ./tests/integration/... -run TestDistributedScheduler_FailoverScenario
```

## Test Scenarios

### 1. End-to-End Flow (5 agents)

**What it tests:**
- Shard manager registration in etcd
- Node registration in Redis
- Kafka queue producer/consumer
- Placement handler logic
- Atomic allocation with Redis Lua scripts

**Expected outcome:**
- All 5 agents successfully placed
- Agents distributed across 3 nodes
- No race conditions or allocation conflicts

**Duration:** ~30 seconds

### 2. Multi-Scheduler Coordination (2 schedulers)

**What it tests:**
- Multiple schedulers joining the cluster
- Consistent hashing agreement
- Exclusive node ownership
- Shard discovery via etcd watch

**Expected outcome:**
- Both schedulers see each other (2 members)
- Consistent node assignment across schedulers
- Exclusive ownership (no overlaps)

**Duration:** ~10 seconds

### 3. Failover Scenario

**What it tests:**
- Scheduler crash detection
- etcd TTL expiry (30s)
- Node reassignment to surviving scheduler
- Consistent hash ring rebuild

**Expected outcome:**
- Failed scheduler removed from cluster
- Nodes reassigned to surviving scheduler
- No downtime in placement operations

**Duration:** ~40 seconds (includes 35s TTL wait)

## Load Testing (Manual)

### Setup Load Test

```bash
# Create load test config
cat > config.loadtest.yaml <<EOF
server:
  address: ":8080"
  enable_auth: false

scheduler:
  mode: distributed
  scheduler_id: load-scheduler-1
  instance_id: load-instance-1
  hostname: localhost
  virtual_nodes: 100
  num_workers: 20

database:
  host: localhost
  port: 5432
  database: aether
  user: aether
  password: aether_dev_password

redis:
  address: localhost:6379
  password: redis_dev_password
  db: 0
  node_ttl: 60s

kafka:
  brokers:
    - localhost:9092
  topic: aether.scheduling.loadtest
  consumer_group: loadtest-group
  num_workers: 20

etcd:
  endpoints:
    - localhost:2379
  key_prefix: /aether/scheduler/shards
  session_ttl: 30
EOF
```

### Run Load Test (1000 agents)

```bash
# Terminal 1: Start scheduler
AETHER_SECURITY_JWT_SECRET_KEY="load-test-secret-key-min-32-chars" \
./aether --config config.loadtest.yaml

# Terminal 2: Generate load
for i in {1..1000}; do
  curl -X POST http://localhost:8080/v1/agents \
    -H "Content-Type: application/json" \
    -d "{
      \"id\": \"agent-$i\",
      \"tenant_id\": \"tenant-$((i % 100))\",
      \"name\": \"Load Test Agent $i\",
      \"image\": \"python:3.11\",
      \"resources\": {
        \"cpu_count\": 1,
        \"memory_mb\": 512
      }
    }"

  if [ $((i % 100)) -eq 0 ]; then
    echo "Created $i agents..."
  fi
done
```

### Monitor Performance

**Kafka UI:** http://localhost:8090
- Consumer lag
- Messages per second
- Partition distribution

**Redis Commander:** http://localhost:8091
- Node count
- Memory usage
- Key distribution

**Metrics:**
```bash
# Watch scheduler stats
watch -n 1 'curl -s http://localhost:9090/metrics | grep scheduler_'

# Check Kafka lag
docker exec aether-kafka kafka-consumer-groups \
  --bootstrap-server localhost:9092 \
  --group loadtest-group \
  --describe
```

## Performance Targets

| Metric | Target | Measurement |
|--------|--------|-------------|
| Placement Latency (p50) | < 10ms | End-to-end |
| Placement Latency (p99) | < 100ms | End-to-end |
| Throughput (single scheduler) | > 100 placements/sec | Sustained |
| Throughput (3 schedulers) | > 300 placements/sec | Sustained |
| Kafka Consumer Lag | < 1000 messages | Steady state |
| Max Concurrent Agents | > 10,000 | Per cluster |
| Memory per 1000 agents | < 100MB | Redis |

## Troubleshooting

### Services Not Healthy

```bash
# Check logs
docker-compose -f docker-compose.distributed.yaml logs kafka
docker-compose -f docker-compose.distributed.yaml logs redis
docker-compose -f docker-compose.distributed.yaml logs etcd

# Restart specific service
docker-compose -f docker-compose.distributed.yaml restart kafka
```

### Test Timeouts

Increase test timeout:
```bash
go test -v ./tests/integration/... -timeout 20m
```

### Kafka Connection Refused

Wait for Kafka to fully start (can take 30-60 seconds):
```bash
# Wait for Kafka health check
docker-compose -f docker-compose.distributed.yaml ps kafka

# Manually test connection
docker exec aether-kafka kafka-topics --bootstrap-server localhost:9092 --list
```

### etcd Connection Issues

```bash
# Check etcd health
docker exec aether-etcd etcdctl endpoint health

# List keys
docker exec aether-etcd etcdctl get --prefix "/aether"
```

### Redis Connection Issues

```bash
# Test Redis connection
docker exec aether-redis redis-cli -a redis_dev_password ping

# List keys
docker exec aether-redis redis-cli -a redis_dev_password --no-auth-warning keys "aether:*"
```

## Cleanup

### Stop Infrastructure

```bash
docker-compose -f docker-compose.distributed.yaml down
```

### Remove Data Volumes

```bash
docker-compose -f docker-compose.distributed.yaml down -v
```

### Remove All

```bash
docker-compose -f docker-compose.distributed.yaml down -v --remove-orphans
```

## Debugging Tools

### Kafka Topics

```bash
# List topics
docker exec aether-kafka kafka-topics --bootstrap-server localhost:9092 --list

# Describe topic
docker exec aether-kafka kafka-topics \
  --bootstrap-server localhost:9092 \
  --describe \
  --topic aether.scheduling.requests

# Consume messages (for debugging)
docker exec aether-kafka kafka-console-consumer \
  --bootstrap-server localhost:9092 \
  --topic aether.scheduling.requests \
  --from-beginning \
  --max-messages 10
```

### etcd Keys

```bash
# List all scheduler shards
docker exec aether-etcd etcdctl get --prefix "/aether/scheduler/shards/" --keys-only

# Get shard details
docker exec aether-etcd etcdctl get "/aether/scheduler/shards/scheduler-1"

# Watch for changes
docker exec aether-etcd etcdctl watch --prefix "/aether/scheduler/shards/"
```

### Redis Keys

```bash
# List all nodes
docker exec aether-redis redis-cli -a redis_dev_password --no-auth-warning keys "aether:node:*"

# Get node details
docker exec aether-redis redis-cli -a redis_dev_password --no-auth-warning get "aether:node:node-001"

# List nodes by scheduler
docker exec aether-redis redis-cli -a redis_dev_password --no-auth-warning smembers "aether:scheduler:scheduler-1:nodes"
```

## CI/CD Integration

### GitHub Actions Example

```yaml
name: Distributed Tests

on: [push, pull_request]

jobs:
  distributed-tests:
    runs-on: ubuntu-latest

    services:
      postgres:
        image: postgres:15-alpine
        env:
          POSTGRES_DB: aether
          POSTGRES_USER: aether
          POSTGRES_PASSWORD: aether_test
        ports:
          - 5432:5432
        options: >-
          --health-cmd pg_isready
          --health-interval 10s
          --health-timeout 5s
          --health-retries 5

      redis:
        image: redis:7-alpine
        ports:
          - 6379:6379
        options: >-
          --health-cmd "redis-cli ping"
          --health-interval 10s
          --health-timeout 5s
          --health-retries 5

      # Add Kafka, Zookeeper, etcd as needed

    steps:
      - uses: actions/checkout@v3

      - name: Set up Go
        uses: actions/setup-go@v4
        with:
          go-version: '1.21'

      - name: Run distributed tests
        env:
          DISTRIBUTED_INFRA_AVAILABLE: "true"
        run: |
          go test -v ./tests/integration/... -timeout 10m
```

## Next Steps

After successful testing:

1. **Production Deployment**
   - Deploy to staging environment
   - Run load tests with production-like data
   - Monitor metrics and alerts

2. **Operational Runbook**
   - Document failure scenarios
   - Create recovery procedures
   - Define SLOs and alerts

3. **Capacity Planning**
   - Measure resource usage per 1000 agents
   - Determine scaling thresholds
   - Plan infrastructure sizing

4. **Migration Strategy**
   - Implement feature flag for distributed mode
   - Deploy canary (10% traffic)
   - Gradual rollout plan
