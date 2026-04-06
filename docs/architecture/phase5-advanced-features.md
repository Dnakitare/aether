# Phase 5: Advanced Features

**Status**: ✅ Complete
**Timeline**: Weeks 13-16
**Goal**: Agent communication, state management, recovery, and network routing

## Overview

Phase 5 implements the advanced features that enable sophisticated multi-agent systems at production scale:

- **Event-driven Messaging**: Kafka-based pub/sub and direct agent communication
- **Rate Limiting**: Multi-tier, multi-layer token bucket algorithm with Redis
- **Agent Recovery**: PostgreSQL-backed checkpointing with multiple recovery strategies
- **Network Routing**: Service discovery, load balancing, and circuit breaker patterns
- **Connection Pooling**: Efficient connection management for agent-to-agent communication

These capabilities transform Aether from a single-agent runtime into a distributed multi-agent orchestration platform.

## Architecture

### Event-Driven Messaging (Kafka)

```
┌─────────────────────────────────────────────────────────────┐
│                    Kafka Event Bus                          │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│  Topics:                                                    │
│  ├── aether.events.{event-type}   (pub/sub broadcast)     │
│  ├── aether.agent.{agent-id}      (direct messaging)      │
│  └── aether.tenant.{tenant-id}    (tenant-scoped events)  │
│                                                             │
└─────────────────────────────────────────────────────────────┘
         ▲                    ▲                    ▲
         │                    │                    │
    ┌────┴────┐          ┌────┴────┐          ┌────┴────┐
    │ Agent 1 │          │ Agent 2 │          │ Agent 3 │
    │ Publish │          │ Subscribe│         │Subscribe│
    └─────────┘          └─────────┘          └─────────┘
```

**Key Features**:
- **Topic-based Pub/Sub**: Agents publish events to topics, subscribers receive them
- **Direct Messaging**: Agent-to-agent communication via dedicated topics
- **Message Envelopes**: Structured messages with metadata (from, to, timestamp)
- **Compression**: Snappy, gzip, lz4, zstd compression for efficiency
- **Consumer Groups**: Load distribution across multiple consumers
- **Batching**: Automatic batching for throughput optimization

**Implementation** (`internal/messaging/kafka.go`):
```go
// Publish to topic (broadcast)
pubsub := messaging.NewPubSub(logger, kafkaClient)
pubsub.Publish(ctx, "events.data", map[string]string{
    "type": "data-ready",
    "source": "agent-1",
})

// Subscribe to topic
handler := func(msg interface{}) error {
    // Process message
    return nil
}
pubsub.Subscribe(ctx, "events.data", "consumer-group-1", handler)

// Direct agent-to-agent messaging
dm := messaging.NewDirectMessaging(logger, kafkaClient)
dm.SendMessage(ctx, "agent-1", "agent-2", map[string]string{
    "command": "execute",
    "payload": "task-123",
})
```

**Configuration**:
```go
config := messaging.DefaultKafkaConfig()
// Brokers: localhost:9092
// TopicPrefix: aether.
// NumPartitions: 3
// ReplicationFactor: 1
// BatchSize: 100
// BatchTimeout: 10ms
// Compression: snappy
```

### Rate Limiting (Token Bucket)

```
┌─────────────────────────────────────────────────────────────┐
│                  Multi-Layer Rate Limiting                   │
└─────────────────────────────────────────────────────────────┘
         │
         ├── Layer 1: Tenant Level (tier-based)
         │   ├── Free: 10 req/s, burst 20
         │   ├── Pro: 100 req/s, burst 200
         │   └── Enterprise: 1000 req/s, burst 2000
         │
         ├── Layer 2: User Level (10% of tenant limit)
         │   └── Prevents single user consuming tenant quota
         │
         └── Layer 3: Endpoint Level (cost-based)
             ├── GET /agents (cost: 1)
             ├── POST /agents (cost: 5)
             └── POST /agents/:id/exec (cost: 10)
```

**Token Bucket Algorithm**:
```
Tokens = min(Burst, Tokens + (TimeSinceLastRefill × Rate))
If Tokens >= Requested:
    Tokens -= Requested
    Allow Request
Else:
    Reject Request (429 Too Many Requests)
```

**Implementation** (`internal/ratelimit/tokenbucket.go`):

Uses Redis Lua script for atomic token bucket operations:
```lua
-- Get current tokens and last refill time
local tokens = redis.call('GET', tokens_key) or burst
local last_refill = redis.call('GET', last_refill_key) or now

-- Calculate tokens to add based on elapsed time
local elapsed = now - last_refill
local tokens_to_add = elapsed * rate
tokens = math.min(burst, tokens + tokens_to_add)

-- Check if request allowed
if tokens >= requested then
    tokens = tokens - requested
    allowed = 1
end

-- Update Redis
redis.call('SET', tokens_key, tokens, 'EX', 3600)
redis.call('SET', last_refill_key, now, 'EX', 3600)

return {allowed, tokens, reset_at}
```

**HTTP Middleware**:
```go
limiter := ratelimit.NewMultiLayerLimiter(logger, redisClient, ratelimit.DefaultTierLimits())
handler := limiter.Middleware(yourHandler)

// Automatic headers in response:
// X-RateLimit-Limit: 100
// X-RateLimit-Remaining: 87
// X-RateLimit-Reset: 1704067200
// Retry-After: 13 (if rate limited)
```

**Usage**:
```go
// Check rate limit
result, err := limiter.Check(ctx, tenantID, userID, endpoint, cost)
if err != nil {
    return err
}
if !result.Allowed {
    // Return 429 with Retry-After header
    return TooManyRequestsError
}
```

### Agent Recovery (Checkpointing)

```
┌─────────────────────────────────────────────────────────────┐
│                PostgreSQL Checkpoint Storage                 │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│  Checkpoints Table:                                         │
│  ├── agent_id (indexed)                                    │
│  ├── tenant_id (indexed)                                   │
│  ├── version (auto-increment)                              │
│  ├── state (JSONB)                                         │
│  ├── metadata (JSONB)                                      │
│  └── created_at (timestamp)                                │
│                                                             │
│  Retention: Keep last N checkpoints per agent              │
│                                                             │
└─────────────────────────────────────────────────────────────┘
```

**Recovery Strategies**:

1. **Restart**: Restore from latest checkpoint and restart agent
   ```go
   strategy := recovery.StrategyRestart
   ```

2. **Failover**: Move agent to different host and restore state
   ```go
   strategy := recovery.StrategyFailover
   ```

3. **Rollback**: Restore from previous checkpoint version
   ```go
   strategy := recovery.StrategyRollback
   ```

**Implementation** (`internal/recovery/checkpoint.go`):

```go
// Create checkpoint
checkpoint := &recovery.Checkpoint{
    AgentID:  agentID,
    TenantID: tenantID,
    Version:  version,
    State: map[string]interface{}{
        "status": "running",
        "memory": agentMemory,
        "context": conversationContext,
    },
    Metadata: map[string]string{
        "source": "auto",
        "trigger": "scheduled",
    },
}
cm.CreateCheckpoint(ctx, checkpoint)

// Recover agent
rm := recovery.NewRecoveryManager(logger, cm, config)
err := rm.RecoverAgentWithRetry(ctx, agentID, func(state map[string]interface{}) error {
    // Restore agent state
    agent.RestoreMemory(state["memory"])
    agent.RestoreContext(state["context"])
    return nil
})
```

**Automatic Checkpointing**:
```go
config := recovery.DefaultCheckpointConfig()
// Interval: 5 minutes
// RetentionCount: 10 (keep last 10 checkpoints)
// MaxCheckpointSize: 10MB

// Cleanup runs after creating new checkpoint
cm.CreateCheckpoint(ctx, checkpoint)  // Also triggers cleanup
```

**Recovery with Retry**:
```go
config := recovery.DefaultRecoveryConfig()
// MaxRetries: 3
// RetryDelay: 5s (exponential backoff)
// HealthCheckInterval: 10s

rm.RecoverAgentWithRetry(ctx, agentID, restoreFunc)
// Retries up to 3 times with exponential backoff
```

### Network Routing

```
┌─────────────────────────────────────────────────────────────┐
│                    Service Registry                          │
├─────────────────────────────────────────────────────────────┤
│  Agent ID │ Address       │ Health    │ Last Check          │
│  agent-1  │ 10.0.1.10     │ healthy   │ 2026-02-01 10:00   │
│  agent-2  │ 10.0.1.11     │ healthy   │ 2026-02-01 10:00   │
│  agent-3  │ 10.0.1.12     │ unhealthy │ 2026-02-01 09:58   │
└─────────────────────────────────────────────────────────────┘
         │
         ├── Load Balancer
         │   ├── Round-Robin (default)
         │   ├── Least Connections
         │   └── Random
         │
         ├── Circuit Breaker
         │   ├── Closed → Open (after N failures)
         │   ├── Open → Half-Open (after cooldown)
         │   └── Half-Open → Closed (after N successes)
         │
         └── Connection Pool
             └── Per-agent connection management
```

**Load Balancing Strategies**:

1. **Round-Robin**: Distribute requests evenly across agents
   ```go
   lb := routing.NewLoadBalancer(logger, router, routing.StrategyRoundRobin)
   ```

2. **Least Connections**: Route to agent with fewest active connections
   ```go
   lb := routing.NewLoadBalancer(logger, router, routing.StrategyLeastConnections)
   ```

3. **Random**: Randomly select healthy agent
   ```go
   lb := routing.NewLoadBalancer(logger, router, routing.StrategyRandom)
   ```

**Circuit Breaker Pattern**:

```
Closed (normal operation)
   │
   ├─► Record Success → Stay Closed
   │
   └─► Record Failure → failures++
       │
       └─► If failures >= threshold
           │
           └─► Open (reject requests)
               │
               └─► After cooldown period
                   │
                   └─► Half-Open (test recovery)
                       │
                       ├─► Success → Closed
                       │
                       └─► Failure → Open
```

**Implementation** (`internal/routing/router.go`):

```go
// Register agent
router.Register(ctx, agentID, tenantID, "10.0.1.10", 8080, metadata)

// Update health status
router.UpdateHealth(agentID, true)  // healthy
router.UpdateHealth(agentID, false) // unhealthy (increments failure count)

// Select agent with load balancer
lb := routing.NewLoadBalancer(logger, router, routing.StrategyRoundRobin)
agent, err := lb.SelectAgent(tenantID)

// Circuit breaker
policy := routing.DefaultTrafficPolicy()
cb := routing.NewCircuitBreaker(logger, policy)

if err := cb.Allow(); err != nil {
    // Circuit open, reject request
    return CircuitOpenError
}

// Make request
err = makeRequest(agent)
if err != nil {
    cb.RecordFailure()
} else {
    cb.RecordSuccess()
}
```

**Connection Pooling**:
```go
config := routing.DefaultConnectionPoolConfig()
// MaxConnections: 100
// MaxIdleTime: 5 minutes
// CleanupInterval: 1 minute

pool := routing.NewConnectionPool(logger, config)

// Get connection
if err := pool.GetConnection(agentID); err != nil {
    return NoConnectionsAvailableError
}

// Release connection
pool.ReleaseConnection(agentID)

// Automatic cleanup of idle connections
```

**Traffic Policies**:
```go
policy := routing.DefaultTrafficPolicy()
// MaxRetries: 3
// ConnectionTimeout: 30s
// RequestTimeout: 60s
// FailureThreshold: 5
// CircuitOpenDuration: 30s
```

## Deployment

### Infrastructure (Docker Compose)

**Standalone Kafka** (`deployments/kafka.yaml`):
```yaml
services:
  zookeeper:
    image: confluentinc/cp-zookeeper:7.5.0
    ports: ["2181:2181"]

  kafka:
    image: confluentinc/cp-kafka:7.5.0
    ports: ["9092:9092"]
    depends_on: [zookeeper]

  kafka-ui:
    image: provectuslabs/kafka-ui:latest
    ports: ["8080:8080"]
    depends_on: [kafka]
```

**Complete Phase 5 Stack** (`deployments/phase5.yaml`):
```yaml
services:
  zookeeper:
    image: confluentinc/cp-zookeeper:7.5.0

  kafka:
    image: confluentinc/cp-kafka:7.5.0
    healthcheck:
      test: ["CMD", "kafka-broker-api-versions", "--bootstrap-server", "localhost:9092"]

  kafka-ui:
    image: provectuslabs/kafka-ui:latest
    ports: ["8080:8080"]

  redis:
    image: redis:7-alpine
    command: redis-server --appendonly yes --maxmemory 512mb --maxmemory-policy allkeys-lru
    ports: ["6379:6379"]
    healthcheck:
      test: ["CMD", "redis-cli", "ping"]

  postgres:
    image: postgres:16-alpine
    ports: ["5432:5432"]
    environment:
      POSTGRES_USER: aether
      POSTGRES_PASSWORD: aether
      POSTGRES_DB: aether
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U aether"]

  redis-commander:
    image: rediscommander/redis-commander:latest
    ports: ["8082:8081"]
```

### Start Infrastructure

```bash
# Start complete Phase 5 stack
docker-compose -f deployments/phase5.yaml up -d

# Verify services
docker-compose -f deployments/phase5.yaml ps

# View Kafka UI
open http://localhost:8080

# View Redis Commander
open http://localhost:8082

# Check PostgreSQL
psql -h localhost -U aether -d aether
```

## Usage Examples

### Event-Driven Agent Communication

**Scenario**: Data processing pipeline with multiple agents

```go
import (
    "github.com/dnakitare/aether/internal/messaging"
    "github.com/dnakitare/aether/pkg/api"
)

// Setup
logger := slog.Default()
kafkaClient, _ := messaging.NewKafkaClient(logger, messaging.DefaultKafkaConfig())
pubsub := messaging.NewPubSub(logger, kafkaClient)

// Agent 1: Data ingestion
go func() {
    pubsub.Publish(ctx, "events.data-ingested", map[string]interface{}{
        "source": "api",
        "records": 1000,
        "timestamp": time.Now(),
    })
}()

// Agent 2: Data processing
go func() {
    handler := func(msg interface{}) error {
        data := msg.(map[string]interface{})
        // Process data
        log.Printf("Processing %d records", data["records"])

        // Publish processed event
        pubsub.Publish(ctx, "events.data-processed", map[string]interface{}{
            "status": "complete",
            "records": data["records"],
        })
        return nil
    }
    pubsub.Subscribe(ctx, "events.data-ingested", "processors", handler)
}()

// Agent 3: Analytics
go func() {
    handler := func(msg interface{}) error {
        data := msg.(map[string]interface{})
        // Generate analytics
        log.Printf("Analytics ready for %d records", data["records"])
        return nil
    }
    pubsub.Subscribe(ctx, "events.data-processed", "analytics", handler)
}()
```

### Direct Agent-to-Agent Messaging

**Scenario**: Coordinator agent dispatching tasks

```go
dm := messaging.NewDirectMessaging(logger, kafkaClient)

// Coordinator agent
coordinator := api.AgentID("coordinator-1")
workers := []api.AgentID{"worker-1", "worker-2", "worker-3"}

// Dispatch tasks
for i, worker := range workers {
    dm.SendMessage(ctx, coordinator, worker, map[string]interface{}{
        "task_id": fmt.Sprintf("task-%d", i),
        "action": "process",
        "priority": "high",
    })
}

// Worker receives messages
handler := func(envelope messaging.MessageEnvelope) error {
    task := envelope.Payload.(map[string]interface{})
    log.Printf("Worker %s received task %s from %s",
        envelope.To, task["task_id"], envelope.From)

    // Send result back to coordinator
    dm.SendMessage(ctx, envelope.To, envelope.From, map[string]interface{}{
        "task_id": task["task_id"],
        "status": "completed",
        "result": "success",
    })
    return nil
}
dm.ReceiveMessages(ctx, "worker-1", handler)
```

### Rate Limiting in API

**Scenario**: Multi-tenant API with tier-based limits

```go
import (
    "github.com/dnakitare/aether/internal/ratelimit"
)

// Setup
redisClient := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
limiter := ratelimit.NewMultiLayerLimiter(logger, redisClient, ratelimit.DefaultTierLimits())

// HTTP middleware
mux := http.NewServeMux()
mux.HandleFunc("/v1/agents", createAgentHandler)

// Apply rate limiting
handler := limiter.Middleware(mux)
http.ListenAndServe(":8080", handler)

// Headers in response:
// X-RateLimit-Limit: 100
// X-RateLimit-Remaining: 87
// X-RateLimit-Reset: 1704067200
```

**Custom tier limits**:
```go
customLimits := map[ratelimit.Tier]ratelimit.Limit{
    ratelimit.TierFree: {
        Rate:   5,              // 5 requests/second
        Burst:  10,             // Allow burst up to 10
        Period: time.Second,
    },
    ratelimit.TierPro: {
        Rate:   50,
        Burst:  100,
        Period: time.Second,
    },
    "custom-enterprise": {
        Rate:   10000,
        Burst:  20000,
        Period: time.Second,
    },
}
limiter := ratelimit.NewMultiLayerLimiter(logger, redisClient, customLimits)
```

### Agent Recovery with Checkpoints

**Scenario**: Long-running agent with periodic checkpointing

```go
import (
    "github.com/dnakitare/aether/internal/recovery"
)

// Setup
db, _ := sql.Open("postgres", "postgres://aether:aether@localhost:5432/aether?sslmode=disable")
cm := recovery.NewCheckpointManager(logger, db, recovery.DefaultCheckpointConfig())
rm := recovery.NewRecoveryManager(logger, cm, recovery.DefaultRecoveryConfig())

// Agent running
agentID := api.AgentID("long-running-1")
tenantID := api.TenantID("acme-corp")

// Create checkpoint every 5 minutes
ticker := time.NewTicker(5 * time.Minute)
go func() {
    for range ticker.C {
        checkpoint := &recovery.Checkpoint{
            AgentID:  agentID,
            TenantID: tenantID,
            State: map[string]interface{}{
                "current_step": currentStep,
                "memory": agentMemory,
                "context": conversationContext,
            },
            Metadata: map[string]string{
                "source": "auto",
                "trigger": "scheduled",
            },
        }
        cm.CreateCheckpoint(ctx, checkpoint)
    }
}()

// Agent crashes, recover
err := rm.RecoverAgentWithRetry(ctx, agentID, func(state map[string]interface{}) error {
    currentStep = state["current_step"].(int)
    agentMemory = state["memory"]
    conversationContext = state["context"]

    // Resume agent from checkpoint
    return agent.Resume()
})
```

**Manual checkpoint for critical operations**:
```go
// Before risky operation
checkpoint := &recovery.Checkpoint{
    AgentID:  agentID,
    TenantID: tenantID,
    State:    currentState,
    Metadata: map[string]string{
        "source": "manual",
        "trigger": "before-risky-operation",
    },
}
cm.CreateCheckpoint(ctx, checkpoint)

// Perform risky operation
if err := riskyOperation(); err != nil {
    // Rollback to checkpoint
    rm.RecoverAgent(ctx, agentID)
}
```

### Load Balancing and Circuit Breaker

**Scenario**: Distribute requests across multiple agents with failure handling

```go
import (
    "github.com/dnakitare/aether/internal/routing"
)

// Setup
router := routing.NewRouter(logger, routing.DefaultRouterConfig())
lb := routing.NewLoadBalancer(logger, router, routing.StrategyRoundRobin)

// Register agents
tenantID := api.TenantID("acme-corp")
for i := 1; i <= 3; i++ {
    agentID := api.AgentID(fmt.Sprintf("worker-%d", i))
    router.Register(ctx, agentID, tenantID, "10.0.1."+strconv.Itoa(10+i), 8080, nil)
    router.UpdateHealth(agentID, true)
}

// Circuit breaker
policy := routing.DefaultTrafficPolicy()
cb := routing.NewCircuitBreaker(logger, policy)

// Handle requests
for i := 0; i < 100; i++ {
    // Check circuit breaker
    if err := cb.Allow(); err != nil {
        log.Printf("Circuit open, rejecting request")
        continue
    }

    // Select agent
    agent, err := lb.SelectAgent(tenantID)
    if err != nil {
        log.Printf("No healthy agents available")
        cb.RecordFailure()
        continue
    }

    // Make request
    if err := makeRequest(agent); err != nil {
        router.UpdateHealth(agent.AgentID, false)
        cb.RecordFailure()
    } else {
        cb.RecordSuccess()
    }
}
```

## Testing

### Unit Tests

```bash
# Run all Phase 5 tests
go test ./internal/ratelimit/... ./internal/messaging/... ./internal/recovery/... ./internal/routing/...

# Run specific package
go test ./internal/ratelimit/

# With coverage
go test -cover ./internal/ratelimit/...
```

### Integration Tests

**Requires running infrastructure** (Redis, Kafka, PostgreSQL):

```bash
# Start infrastructure
docker-compose -f deployments/phase5.yaml up -d

# Run integration tests (not in short mode)
go test ./internal/ratelimit/
go test ./internal/messaging/
go test ./internal/recovery/

# Stop infrastructure
docker-compose -f deployments/phase5.yaml down
```

### Load Testing

**Rate Limiting**:
```bash
# Install vegeta
go install github.com/tsenart/vegeta@latest

# Load test rate limiting
echo "GET http://localhost:8080/v1/agents" | vegeta attack -duration=30s -rate=200 | vegeta report

# Should see 429 responses when rate limit exceeded
```

**Kafka Throughput**:
```bash
# Publish 10k messages
for i in {1..10000}; do
    echo "Message $i"
done | kafka-console-producer --broker-list localhost:9092 --topic aether.test

# Measure consumer throughput
kafka-consumer-perf-test --broker-list localhost:9092 --topic aether.test --messages 10000
```

## Monitoring and Observability

### Metrics

**Rate Limiting**:
- `aether_ratelimit_requests_total{tier,allowed}` - Total requests by tier and outcome
- `aether_ratelimit_tokens_remaining{tenant_id}` - Current token count per tenant
- `aether_ratelimit_rejections_total{tier}` - Total rejections by tier

**Messaging**:
- `aether_kafka_messages_published_total{topic}` - Messages published per topic
- `aether_kafka_messages_consumed_total{topic,consumer_group}` - Messages consumed
- `aether_kafka_consumer_lag{topic,partition}` - Consumer lag

**Recovery**:
- `aether_checkpoint_creates_total{tenant_id}` - Checkpoints created
- `aether_recovery_attempts_total{strategy,outcome}` - Recovery attempts
- `aether_checkpoint_size_bytes{agent_id}` - Checkpoint size

**Routing**:
- `aether_router_agents_total{health}` - Registered agents by health status
- `aether_loadbalancer_requests_total{strategy}` - Load balancer requests
- `aether_circuitbreaker_state{agent_id}` - Circuit breaker state
- `aether_connection_pool_active{agent_id}` - Active connections

### Logs

All components use structured logging with OpenTelemetry trace correlation:

```go
slog.InfoContext(ctx, "rate limit check",
    "tenant_id", tenantID,
    "allowed", result.Allowed,
    "remaining", result.Remaining,
)
```

**View logs with trace correlation**:
```bash
# Filter by trace ID
grep "trace_id=abc123" logs.json

# Filter by component
grep "component=ratelimit" logs.json
```

### Dashboards

**Kafka UI** (http://localhost:8080):
- Topics and partitions
- Consumer groups and lag
- Message browser
- Broker metrics

**Redis Commander** (http://localhost:8082):
- Key browser
- Memory usage
- Rate limit token buckets
- Connection stats

**Grafana** (if using observability stack):
- Import dashboards from `deployments/grafana/dashboards/phase5-*.json`
- Panels for rate limiting, messaging, recovery, routing

## Best Practices

### Rate Limiting

1. **Choose appropriate tier**: Start with conservative limits, increase based on monitoring
2. **Use cost-based endpoint limits**: Expensive operations (agent exec) should have higher cost
3. **Monitor rejection rate**: High rejection rate indicates tier limits too low
4. **Set reasonable burst**: Burst should be 2x rate to handle traffic spikes
5. **Handle 429 gracefully**: Clients should respect Retry-After header

### Messaging

1. **Use pub/sub for broadcast**: Event notifications, status updates
2. **Use direct messaging for RPC**: Request/response patterns, task dispatch
3. **Set consumer groups**: Distribute load across multiple consumers
4. **Enable compression**: Snappy for speed, gzip for size
5. **Monitor consumer lag**: High lag indicates processing bottleneck

### Recovery

1. **Checkpoint before risky operations**: Manual checkpoints for critical state changes
2. **Set retention based on SLA**: Keep enough checkpoints to meet recovery time objective
3. **Test recovery regularly**: Chaos engineering - kill agents and verify recovery
4. **Monitor checkpoint size**: Large checkpoints indicate too much state
5. **Use appropriate strategy**: Restart for simple failures, failover for host issues

### Routing

1. **Health check frequently**: Detect failures fast with frequent health checks
2. **Choose right strategy**: Round-robin for even load, least connections for variable tasks
3. **Set circuit breaker threshold**: 5 failures is good default, adjust based on SLA
4. **Monitor connection pool**: High active connections indicate need for more agents
5. **Use traffic policies**: Set reasonable timeouts and retry limits

## Security Considerations

### Rate Limiting
- **Tenant isolation**: Each tenant has separate token bucket
- **User limits**: Prevent single user DOS within tenant
- **Endpoint costs**: Protect expensive operations with higher costs
- **Redis security**: Use Redis ACLs and password authentication in production

### Messaging
- **Topic ACLs**: Restrict which agents can publish/subscribe to which topics
- **Message validation**: Validate message structure and size
- **Encryption in transit**: Use TLS for Kafka connections in production
- **Tenant namespacing**: Prefix topics with tenant ID for isolation

### Recovery
- **Checkpoint encryption**: Encrypt sensitive data in checkpoints (PII, credentials)
- **Access control**: Only allow agents to access their own checkpoints
- **Audit logging**: Log all checkpoint creates and recovery attempts
- **Retention compliance**: Set retention based on data retention policies

### Routing
- **Network isolation**: Agents should only route within their tenant namespace
- **Health check authentication**: Secure health check endpoints
- **Connection limits**: Prevent connection pool exhaustion attacks
- **Circuit breaker**: Protect against cascading failures

## Performance Optimization

### Rate Limiting
- **Redis pipelining**: Batch multiple rate limit checks in single round-trip
- **Local caching**: Cache tier limits to avoid Redis lookups
- **Lua script**: Atomic operations eliminate race conditions

### Messaging
- **Batch publishing**: Send multiple messages in single batch (100 messages, 10ms timeout)
- **Compression**: Snappy provides 3-5x compression with minimal CPU
- **Consumer parallelism**: Multiple consumers in consumer group for throughput

### Recovery
- **Async checkpointing**: Don't block agent on checkpoint creation
- **Incremental snapshots**: Only save changed state (future optimization)
- **Compression**: Compress checkpoint state with gzip

### Routing
- **Connection pooling**: Reuse connections instead of creating new ones
- **Health check batching**: Check multiple agents in parallel
- **Circuit breaker**: Fail fast instead of waiting for timeout

## Troubleshooting

### Rate Limiting Issues

**Problem**: Requests getting rate limited unexpectedly
```bash
# Check current token count
redis-cli GET "ratelimit:tenant:acme-corp:tokens"

# Check tier configuration
grep "TierLimits" internal/ratelimit/tokenbucket.go

# Check rejection count
curl -I http://localhost:8080/v1/agents
# Look at X-RateLimit-Remaining header
```

### Messaging Issues

**Problem**: Messages not being consumed
```bash
# Check consumer group lag
kafka-consumer-groups --bootstrap-server localhost:9092 --group processors --describe

# Check topic exists
kafka-topics --bootstrap-server localhost:9092 --list

# View messages
kafka-console-consumer --bootstrap-server localhost:9092 --topic aether.events.data --from-beginning
```

### Recovery Issues

**Problem**: Agent recovery failing
```bash
# Check checkpoints exist
psql -h localhost -U aether -d aether -c "SELECT agent_id, version, created_at FROM checkpoints WHERE agent_id='agent-1' ORDER BY created_at DESC LIMIT 5;"

# Check checkpoint size
psql -h localhost -U aether -d aether -c "SELECT agent_id, pg_column_size(state) as size_bytes FROM checkpoints WHERE agent_id='agent-1' ORDER BY created_at DESC LIMIT 1;"

# Test recovery manually
go run cmd/test-recovery/main.go --agent-id=agent-1
```

### Routing Issues

**Problem**: Load balancer not distributing evenly
```bash
# Check agent health
curl http://localhost:8080/v1/router/services | jq '.[] | {agent_id, health}'

# Check load balancer strategy
grep "StrategyRoundRobin" internal/routing/router.go

# Check circuit breaker state
curl http://localhost:8080/v1/router/circuit-breaker/agent-1 | jq '.state'
```

## Migration Guide

### Upgrading from Phase 4

Phase 5 is additive - no breaking changes to existing APIs.

**New dependencies**:
```bash
go get github.com/segmentio/kafka-go@latest
# Redis and PostgreSQL clients already installed in Phase 4
```

**New infrastructure**:
```bash
# Add Kafka to existing stack
docker-compose -f deployments/phase5.yaml up -d kafka zookeeper kafka-ui

# Or start complete Phase 5 stack
docker-compose -f deployments/phase5.yaml up -d
```

**Database migrations**:
```sql
-- Create checkpoints table
CREATE TABLE IF NOT EXISTS checkpoints (
    id SERIAL PRIMARY KEY,
    agent_id TEXT NOT NULL,
    tenant_id TEXT NOT NULL,
    version INTEGER NOT NULL,
    state JSONB NOT NULL,
    metadata JSONB,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(agent_id, version)
);
CREATE INDEX idx_checkpoints_agent ON checkpoints(agent_id);
CREATE INDEX idx_checkpoints_tenant ON checkpoints(tenant_id);
```

**Enable features**:
```go
// Rate limiting
limiter := ratelimit.NewMultiLayerLimiter(logger, redisClient, ratelimit.DefaultTierLimits())
handler := limiter.Middleware(yourHandler)

// Messaging
kafkaClient, _ := messaging.NewKafkaClient(logger, messaging.DefaultKafkaConfig())
pubsub := messaging.NewPubSub(logger, kafkaClient)

// Recovery
cm := recovery.NewCheckpointManager(logger, db, recovery.DefaultCheckpointConfig())

// Routing
router := routing.NewRouter(logger, routing.DefaultRouterConfig())
```

## What's Next: Phase 6

Phase 6 (Production Hardening) will add:

- **High Availability**: Multi-region, leader election, state replication
- **Disaster Recovery**: Automated backups, cross-region failover
- **Kubernetes Integration**: CRDs, operator, RuntimeClass
- **Multi-Cloud Support**: Terraform modules for AWS, GCP, Azure
- **Performance**: VM pre-warming, connection pooling optimizations
- **Documentation**: Architecture guide, operations runbook, security best practices

Phase 5 provides the foundation for production-scale multi-agent systems. All advanced features are now in place for agent communication, rate limiting, recovery, and routing.
