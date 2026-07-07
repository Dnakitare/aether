# Phase 5: Advanced Features

**Status**: ✅ Complete
**Timeline**: Weeks 13-16
**Goal**: Rate limiting and agent state recovery

## Overview

Phase 5 adds two advanced runtime features:

- **Rate Limiting**: Multi-tier, multi-layer token bucket algorithm with Redis
- **Agent Recovery**: PostgreSQL-backed checkpointing with multiple recovery strategies

These give Aether per-tenant request control and durable agent state recovery within a single-region control plane.

## Architecture

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

## Deployment

### Infrastructure (Docker Compose)

**Phase 5 Stack** (`deployments/phase5.yaml`):
```yaml
services:
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
# Start Phase 5 stack
docker-compose -f deployments/phase5.yaml up -d

# Verify services
docker-compose -f deployments/phase5.yaml ps

# View Redis Commander
open http://localhost:8082

# Check PostgreSQL
psql -h localhost -U aether -d aether
```

## Usage Examples

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

## Testing

### Unit Tests

```bash
# Run all Phase 5 tests
go test ./internal/ratelimit/... ./internal/recovery/...

# Run specific package
go test ./internal/ratelimit/

# With coverage
go test -cover ./internal/ratelimit/...
```

### Integration Tests

**Requires running infrastructure** (Redis, PostgreSQL):

```bash
# Start infrastructure
docker-compose -f deployments/phase5.yaml up -d

# Run integration tests (not in short mode)
go test ./internal/ratelimit/
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

## Monitoring and Observability

### Metrics

**Rate Limiting**:
- `aether_ratelimit_requests_total{tier,allowed}` - Total requests by tier and outcome
- `aether_ratelimit_tokens_remaining{tenant_id}` - Current token count per tenant
- `aether_ratelimit_rejections_total{tier}` - Total rejections by tier

**Recovery**:
- `aether_checkpoint_creates_total{tenant_id}` - Checkpoints created
- `aether_recovery_attempts_total{strategy,outcome}` - Recovery attempts
- `aether_checkpoint_size_bytes{agent_id}` - Checkpoint size

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

**Redis Commander** (http://localhost:8082):
- Key browser
- Memory usage
- Rate limit token buckets
- Connection stats

**Grafana** (if using observability stack):
- Import dashboards from `deployments/grafana/dashboards/phase5-*.json`
- Panels for rate limiting and recovery

## Best Practices

### Rate Limiting

1. **Choose appropriate tier**: Start with conservative limits, increase based on monitoring
2. **Use cost-based endpoint limits**: Expensive operations (agent exec) should have higher cost
3. **Monitor rejection rate**: High rejection rate indicates tier limits too low
4. **Set reasonable burst**: Burst should be 2x rate to handle traffic spikes
5. **Handle 429 gracefully**: Clients should respect Retry-After header

### Recovery

1. **Checkpoint before risky operations**: Manual checkpoints for critical state changes
2. **Set retention based on SLA**: Keep enough checkpoints to meet recovery time objective
3. **Test recovery regularly**: Chaos engineering - kill agents and verify recovery
4. **Monitor checkpoint size**: Large checkpoints indicate too much state
5. **Use appropriate strategy**: Restart for simple failures, failover for host issues

## Security Considerations

### Rate Limiting
- **Tenant isolation**: Each tenant has separate token bucket
- **User limits**: Prevent single user DOS within tenant
- **Endpoint costs**: Protect expensive operations with higher costs
- **Redis security**: Use Redis ACLs and password authentication

### Recovery
- **Checkpoint encryption**: Encrypt sensitive data in checkpoints (PII, credentials)
- **Access control**: Only allow agents to access their own checkpoints
- **Audit logging**: Log all checkpoint creates and recovery attempts
- **Retention compliance**: Set retention based on data retention policies

## Performance Optimization

### Rate Limiting
- **Redis pipelining**: Batch multiple rate limit checks in single round-trip
- **Local caching**: Cache tier limits to avoid Redis lookups
- **Lua script**: Atomic operations eliminate race conditions

### Recovery
- **Async checkpointing**: Don't block agent on checkpoint creation
- **Incremental snapshots**: Only save changed state (future optimization)
- **Compression**: Compress checkpoint state with gzip

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

## Migration Guide

### Upgrading from Phase 4

Phase 5 is additive - no breaking changes to existing APIs.

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

// Recovery
cm := recovery.NewCheckpointManager(logger, db, recovery.DefaultCheckpointConfig())
```

## What's Next: Phase 6

Phase 6 (Hardening) will focus on:

- **Kubernetes Integration**: CRDs, operator, RuntimeClass
- **Performance**: VM pre-warming, connection pooling optimizations
- **Documentation**: Architecture guide, operations runbook, security best practices

Phase 5 puts per-tenant rate limiting and agent state recovery in place for the single-region runtime.
