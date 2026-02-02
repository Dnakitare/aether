# Aether Production Readiness - Remediation Tickets

**Generated:** 2026-02-01
**Total Tickets:** 97
**Estimated Effort:** 15 weeks (2-3 engineers)

---

## 🔴 PHASE 1: Critical Security (Week 1) - BLOCKING

### P0-SEC-001: Implement JWT Authentication Middleware
**Priority:** P0 - BLOCKER
**Severity:** CRITICAL (CVSS 10.0)
**Effort:** 3 days
**File:** `internal/api/middleware.go:68-73`

**Description:**
Complete authentication bypass - all API endpoints are unauthenticated. Anyone can create/delete agents, modify quotas, access logs without credentials.

**Acceptance Criteria:**
- [ ] JWT validation implemented in authMiddleware
- [ ] Extract and validate Bearer token from Authorization header
- [ ] Verify token signature using JWTManager
- [ ] Add claims to request context
- [ ] Health/readiness endpoints excluded from auth
- [ ] Return 401 for missing/invalid tokens
- [ ] All unit tests pass
- [ ] Integration tests verify auth enforcement

**Technical Details:**
```go
// Validate token format: "Bearer <token>"
// Extract claims (tenant_id, user_id, roles)
// Add to context: auth.WithClaims(r.Context(), claims)
```

**Dependencies:** None
**Blocks:** P0-SEC-002, P0-SEC-003

---

### P0-SEC-002: Add Tenant Isolation Checks to All Endpoints
**Priority:** P0 - BLOCKER
**Severity:** CRITICAL (CVSS 9.8)
**Effort:** 2 days
**Files:** `internal/api/handlers.go` (lines 121-190)

**Description:**
Multi-tenancy isolation completely broken. Users can access/delete other tenants' agents by knowing agent IDs.

**Acceptance Criteria:**
- [ ] Extract tenant ID from auth claims in all handlers
- [ ] Verify agent.Config.TenantID == claims.TenantID before operations
- [ ] Return 403 Forbidden on tenant mismatch
- [ ] Log isolation violation attempts
- [ ] Apply to: GetAgent, DeleteAgent, GetAgentLogs, UpdateAgent
- [ ] Unit tests verify isolation
- [ ] Integration tests verify cross-tenant access denied

**Technical Details:**
```go
tenantID, err := auth.GetTenantID(ctx)
if info.Config.TenantID != tenantID {
    s.logger.WarnContext(ctx, "tenant isolation violation", ...)
    return 403
}
```

**Dependencies:** P0-SEC-001
**Blocks:** None

---

### P0-SEC-003: Fix SQL Injection in Backup/Restore
**Priority:** P0 - BLOCKER
**Severity:** CRITICAL (CVSS 9.1)
**Effort:** 1 day
**Files:**
- `internal/backup/backup.go:200`
- `internal/backup/restore.go:255`

**Description:**
Direct string interpolation in SQL queries allows SQL injection if table names are user-controlled.

**Acceptance Criteria:**
- [ ] Whitelist allowed table names
- [ ] Use pq.QuoteIdentifier() for table names
- [ ] Use parameterized queries where possible
- [ ] Validate all inputs against allowlist
- [ ] Unit tests verify injection prevented
- [ ] Security test with malicious input

**Technical Details:**
```go
allowedTables := map[string]bool{"agents": true, "tenants": true, ...}
if !allowedTables[table] { return error }
query := fmt.Sprintf("SELECT COUNT(*) FROM %s", pq.QuoteIdentifier(table))
```

**Dependencies:** None
**Blocks:** None

---

### P0-SEC-004: Fix Command Injection in Network Device Creation
**Priority:** P0 - BLOCKER
**Severity:** CRITICAL (CVSS 9.3)
**Effort:** 1 day
**File:** `internal/runtime/vm/lifecycle.go:260`

**Description:**
Unsanitized network device names passed to shell command, allowing RCE if agent ID is user-controlled.

**Acceptance Criteria:**
- [ ] Validate device name: regex `^[a-zA-Z0-9-]+$`
- [ ] Enforce max length (15 chars for Linux interfaces)
- [ ] Reject invalid characters
- [ ] Unit tests verify validation
- [ ] Security test with malicious input (`$(cmd)`, `; rm -rf`)

**Technical Details:**
```go
if !regexp.MustCompile(`^[a-zA-Z0-9-]+$`).MatchString(name) {
    return fmt.Errorf("invalid device name")
}
if len(name) > 15 { return error }
```

**Dependencies:** None
**Blocks:** None

---

### P0-SEC-005: Implement HashiCorp Vault Integration
**Priority:** P0 - BLOCKER
**Severity:** CRITICAL (CVSS 8.7)
**Effort:** 2 days
**File:** `internal/secrets/vault.go` (create new)

**Description:**
Secrets management not implemented. Credentials stored in plaintext environment variables, exposed in memory dumps and logs.

**Acceptance Criteria:**
- [ ] Create VaultManager with KV v2 engine support
- [ ] Implement GetSecret, PutSecret, DeleteSecret
- [ ] Add connection pooling and retry logic
- [ ] Support token renewal
- [ ] Integrate with agent runtime (inject secrets)
- [ ] Unit tests with mock Vault
- [ ] Integration tests with real Vault

**Technical Details:**
```go
type VaultManager struct {
    client *vault.Client
    config Config
}
func NewVaultManager(config Config) (*VaultManager, error)
func (vm *VaultManager) GetSecret(ctx, path string) (map[string]interface{}, error)
```

**Dependencies:** None
**Blocks:** P0-SEC-006

---

### P0-SEC-006: Remove Hardcoded Credentials from Docker Compose
**Priority:** P0 - BLOCKER
**Severity:** HIGH (CVSS 7.5)
**Effort:** 0.5 days
**File:** `deployments/docker/docker-compose.dev.yml:24`

**Description:**
Database passwords hardcoded in version-controlled Docker Compose file.

**Acceptance Criteria:**
- [ ] Create `.env.dev` file (add to .gitignore)
- [ ] Move all secrets to .env file
- [ ] Use environment variable substitution in compose
- [ ] Update documentation with setup instructions
- [ ] Verify .env.dev NOT in git history

**Technical Details:**
```yaml
environment:
  POSTGRES_PASSWORD: ${POSTGRES_PASSWORD}
env_file:
  - .env.dev
```

**Dependencies:** None
**Blocks:** None

---

### P0-SEC-007: Add Input Validation Framework
**Priority:** P0 - BLOCKER
**Severity:** HIGH (CVSS 8.0)
**Effort:** 1 day
**File:** `internal/api/handlers.go:48-66`

**Description:**
No input validation on API requests allows oversized payloads, invalid resource specs, malicious container images.

**Acceptance Criteria:**
- [ ] Validate agent name: max 64 chars, alphanumeric+dash+underscore
- [ ] Validate image: whitelist approved registries
- [ ] Validate resources: CPU 1-64, Memory 128MB-128GB
- [ ] Add max payload size limit (10MB)
- [ ] Return 400 with validation errors
- [ ] Unit tests for all validation rules

**Technical Details:**
```go
if len(req.Name) > 64 || !regexp.Match(`^[a-zA-Z0-9-_]+$`, req.Name) {
    return 400
}
if !isAllowedImage(req.Image) { return 400 }
```

**Dependencies:** None
**Blocks:** None

---

## 🟠 PHASE 2: Data Integrity (Weeks 2-3)

### P0-DB-001: Implement Database Migration Framework
**Priority:** P0 - BLOCKER
**Severity:** CRITICAL (Operations)
**Effort:** 1 week
**Files:** All schema creation code

**Description:**
No migration framework prevents safe schema evolution. Production deployments require manual SQL execution and downtime.

**Acceptance Criteria:**
- [ ] Install golang-migrate
- [ ] Create migrations directory structure
- [ ] Write initial schema migrations (001_*.sql)
- [ ] Remove CREATE TABLE IF NOT EXISTS from code
- [ ] Add migration runner to main.go startup
- [ ] Add rollback migrations (*.down.sql)
- [ ] CI checks verify migrations apply cleanly
- [ ] Documentation for creating new migrations

**Technical Details:**
```
migrations/
├── 001_initial_schema.up.sql
├── 001_initial_schema.down.sql
├── 002_add_checkpoints.up.sql
├── 002_add_checkpoints.down.sql
```

**Dependencies:** None
**Blocks:** All future schema changes

---

### P0-DB-002: Add Ownership Tokens to Distributed Locks
**Priority:** P0 - BLOCKER
**Severity:** CRITICAL (Data Corruption)
**Effort:** 3 days
**File:** `internal/state/redis.go:152-179`

**Description:**
Redis locks have no ownership token. Any process can release any lock, causing race conditions and data corruption.

**Acceptance Criteria:**
- [ ] Generate unique lock token (UUID) on acquire
- [ ] Store token as lock value in Redis
- [ ] Verify token ownership before unlock (Lua script)
- [ ] Implement watchdog for TTL extension
- [ ] Return token to caller
- [ ] Update all Lock/Unlock call sites
- [ ] Unit tests verify ownership enforcement
- [ ] Concurrency tests verify no race conditions

**Technical Details:**
```go
token := uuid.New().String()
ok := SET key token NX EX ttl
// Unlock with Lua: if GET key == token then DEL key
```

**Dependencies:** None
**Blocks:** None

---

### P0-DB-003: Wrap Checkpoint Operations in Transactions
**Priority:** P0 - BLOCKER
**Severity:** HIGH (Data Loss)
**Effort:** 2 days
**File:** `internal/recovery/checkpoint.go:101-169`

**Description:**
Checkpoint creation and cleanup are separate operations. Process crash between them causes orphaned data or missing checkpoints.

**Acceptance Criteria:**
- [ ] Begin transaction before checkpoint insert
- [ ] Delete old checkpoints in same transaction
- [ ] Commit atomically
- [ ] Rollback on any error
- [ ] Use serializable isolation level
- [ ] Unit tests verify atomicity
- [ ] Test rollback on simulated failures

**Technical Details:**
```go
tx, _ := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
defer tx.Rollback()
// INSERT checkpoint
// DELETE old checkpoints
tx.Commit()
```

**Dependencies:** P0-DB-001 (migrations)
**Blocks:** None

---

### P0-DB-004: Fix Checkpoint Version Race Condition
**Priority:** P0 - BLOCKER
**Severity:** HIGH (Unique Constraint Violation)
**Effort:** 2 days
**File:** `internal/recovery/checkpoint.go:302-312`

**Description:**
getLatestVersion + INSERT not atomic. Concurrent checkpoints can generate duplicate version numbers.

**Acceptance Criteria:**
- [ ] Use database sequence for version numbers
- [ ] CREATE SEQUENCE per agent or global
- [ ] Use nextval() in INSERT
- [ ] Remove getLatestVersion query
- [ ] Unit tests verify no duplicates
- [ ] Concurrency test: 100 simultaneous checkpoints

**Technical Details:**
```sql
CREATE SEQUENCE checkpoint_version_seq;
INSERT INTO checkpoints (version, ...)
VALUES (nextval('checkpoint_version_seq'), ...);
```

**Dependencies:** P0-DB-001 (migrations)
**Blocks:** None

---

### P0-DB-005: Add Transaction Boundaries to State Operations
**Priority:** P0 - BLOCKER
**Severity:** HIGH (State Inconsistency)
**Effort:** 2 days
**File:** `internal/state/redis.go:88-129`

**Description:**
Agent save + tenant index update are separate Redis operations. Errors in second operation cause state inconsistency.

**Acceptance Criteria:**
- [ ] Use Redis transactions (MULTI/EXEC)
- [ ] Or use Lua script for atomic operations
- [ ] Wrap: SaveAgent + SAdd tenant set
- [ ] Wrap: DeleteAgent + SRem tenant set
- [ ] Return errors, don't swallow
- [ ] Unit tests verify atomicity

**Technical Details:**
```go
pipe := rs.client.Pipeline()
pipe.Set(ctx, agentKey, data, 0)
pipe.SAdd(ctx, tenantKey, agentID)
_, err := pipe.Exec(ctx)
```

**Dependencies:** None
**Blocks:** None

---

## 🟡 PHASE 3: Resource Management (Week 4)

### P0-LEAK-001: Fix File Handle Leak in VM Logging
**Priority:** P0 - BLOCKER
**Severity:** CRITICAL (Process Crash)
**Effort:** 1 day
**File:** `internal/runtime/vm/lifecycle.go:119-125`

**Description:**
Log file opened but never closed. After ~1024 VM starts, process hits file descriptor limit and crashes.

**Acceptance Criteria:**
- [ ] Store logFile in VM struct
- [ ] Close logFile in Stop() method
- [ ] Use defer if opening in function scope
- [ ] Handle close errors
- [ ] Unit test verifies file closed
- [ ] Integration test: create/destroy 2000 VMs

**Technical Details:**
```go
v.logFile = logFile  // Store reference
// In Stop():
if v.logFile != nil {
    v.logFile.Close()
}
```

**Dependencies:** None
**Blocks:** None

---

### P0-LEAK-002: Fix Tap Device Leak on VM Creation Failure
**Priority:** P0 - BLOCKER
**Severity:** CRITICAL (System Reboot Required)
**Effort:** 1 day
**File:** `internal/runtime/vm/lifecycle.go:83-89`

**Description:**
Tap devices created but not cleaned up if VM creation fails. Device exhaustion requires system reboot.

**Acceptance Criteria:**
- [ ] Track created devices in slice
- [ ] On error, cleanup all created devices
- [ ] Add deleteTapDevice method
- [ ] Use defer for cleanup
- [ ] Unit test verifies cleanup on error
- [ ] Test: fail after creating 3 of 5 devices

**Technical Details:**
```go
var createdDevices []string
defer func() {
    if err != nil {
        for _, dev := range createdDevices {
            m.deleteTapDevice(ctx, dev)
        }
    }
}()
```

**Dependencies:** None
**Blocks:** None

---

### P0-LEAK-003: Fix Goroutine Leak in Log Streaming
**Priority:** P0 - BLOCKER
**Severity:** CRITICAL (Memory Exhaustion)
**Effort:** 0.5 days
**File:** `internal/runtime/agent/logs.go:58-62`

**Description:**
Duplicate case in select statement makes goroutine exit unreachable. Goroutines accumulate until OOM.

**Acceptance Criteria:**
- [ ] Remove duplicate ctx.Done() case
- [ ] Add default case or timeout
- [ ] Verify goroutine exits on context cancel
- [ ] Unit test: verify goroutine cleanup
- [ ] Load test: 1000 log streams, verify no leak

**Technical Details:**
```go
select {
case <-ctx.Done():
    return
default:
    time.Sleep(100 * time.Millisecond)
}
```

**Dependencies:** None
**Blocks:** None

---

### P0-LEAK-004: Fix etcd Session Leak on Error
**Priority:** P0 - BLOCKER
**Severity:** HIGH (Connection Exhaustion)
**Effort:** 0.5 days
**File:** `internal/ha/election.go:61-65`

**Description:**
etcd session not closed if client close fails. Sessions accumulate and exhaust connections.

**Acceptance Criteria:**
- [ ] Always attempt session.Close() even if error
- [ ] Log both errors separately
- [ ] Don't return early on session close error
- [ ] Unit test verifies both closures attempted

**Technical Details:**
```go
var errs []error
if le.session != nil {
    if err := le.session.Close(); err != nil {
        errs = append(errs, err)
    }
}
if le.client != nil {
    if err := le.client.Close(); err != nil {
        errs = append(errs, err)
    }
}
```

**Dependencies:** None
**Blocks:** None

---

### P0-RACE-001: Fix Scheduler Double Allocation Race
**Priority:** P0 - BLOCKER
**Severity:** CRITICAL (Resource Corruption)
**Effort:** 2 days
**File:** `internal/scheduler/scheduler.go:242-248`

**Description:**
Allocate + Dequeue not atomic. Two goroutines can allocate same request to different nodes.

**Acceptance Criteria:**
- [ ] Hold lock through entire allocate+dequeue
- [ ] Verify request still in queue before allocate
- [ ] Use Compare-And-Swap pattern
- [ ] Unit test: concurrent schedule attempts
- [ ] Verify exactly one succeeds

**Technical Details:**
```go
s.mu.Lock()
if s.queue.Peek().ID != req.ID {
    s.mu.Unlock()
    return // Already processed
}
node.Allocate(...)
s.queue.Dequeue()
s.mu.Unlock()
```

**Dependencies:** None
**Blocks:** None

---

### P0-RACE-002: Add Mutex to Node Allocate/Deallocate
**Priority:** P0 - BLOCKER
**Severity:** HIGH (Resource Corruption)
**Effort:** 1 day
**File:** `internal/scheduler/types.go:56-83`

**Description:**
Node.Allocate and Deallocate have no synchronization. Concurrent calls corrupt resource counts.

**Acceptance Criteria:**
- [ ] Add sync.RWMutex to Node struct
- [ ] Lock in Allocate/Deallocate
- [ ] RLock in CanFit/Available
- [ ] Unit tests with concurrent operations
- [ ] Verify resource counts stay consistent

**Technical Details:**
```go
type Node struct {
    mu sync.RWMutex
    // ... existing fields
}
func (n *Node) Allocate(...) {
    n.mu.Lock()
    defer n.mu.Unlock()
    // ...
}
```

**Dependencies:** None
**Blocks:** None

---

### P0-CTX-001: Replace context.Background() with Proper Contexts
**Priority:** P0 - BLOCKER
**Severity:** MEDIUM (Cannot Cancel Operations)
**Effort:** 2 days
**Files:**
- `internal/scheduler/scheduler.go:112,126`
- `internal/ha/replication.go:72`
- `internal/recovery/checkpoint.go:166`

**Description:**
context.Background() used in production code prevents operation cancellation and timeout enforcement.

**Acceptance Criteria:**
- [ ] Add context parameter to all methods using Background()
- [ ] Thread context from API handlers
- [ ] Use context for all database/Redis operations
- [ ] Verify operations respect context cancellation
- [ ] Unit tests verify timeout enforcement

**Technical Details:**
```go
// Before:
func (s *Scheduler) ScheduleAgent(req *AgentRequest) error
// After:
func (s *Scheduler) ScheduleAgent(ctx context.Context, req *AgentRequest) error
```

**Dependencies:** None
**Blocks:** None

---

## 🔵 PHASE 4: Scalability (Weeks 5-8)

### P1-SCALE-001: Implement Distributed Scheduler
**Priority:** P1 - Required for >1K agents
**Severity:** HIGH (Scale Limit)
**Effort:** 3 weeks
**File:** `internal/scheduler/scheduler.go`

**Description:**
In-memory scheduler cannot scale beyond 1,000 agents. No persistence means lost state on crash.

**Acceptance Criteria:**
- [ ] Move node state to Redis/etcd
- [ ] Implement distributed queue (Kafka/RabbitMQ)
- [ ] Event-driven scheduling (not polling)
- [ ] Shard scheduler by tenant hash
- [ ] Support multiple scheduler instances
- [ ] Optimistic locking for node allocation
- [ ] Unit tests for distributed operations
- [ ] Load test: 10,000 agents across 3 schedulers

**Technical Details:**
```go
type DistributedScheduler struct {
    stateStore   StateStore  // Redis for placement
    workQueue    WorkQueue   // Kafka for requests
    leaderElect  *ha.LeaderElection
    shardID      int
    shardCount   int
}
```

**Dependencies:** P0-DB-002 (distributed locks)
**Blocks:** Scale >1,000 agents

---

### P1-SCALE-002: Implement Configuration Management
**Priority:** P1 - Required for Multi-Environment
**Severity:** MEDIUM
**Effort:** 1 week
**File:** `cmd/aether/main.go:90-103`

**Description:**
All configuration hardcoded. Cannot run in containers or multiple environments.

**Acceptance Criteria:**
- [ ] Install viper for config management
- [ ] Support config files (YAML/JSON)
- [ ] Support environment variables (AETHER_* prefix)
- [ ] Support command-line flags
- [ ] Validate all configuration on startup
- [ ] Document all config options
- [ ] Unit tests for config loading/validation

**Technical Details:**
```go
type Config struct {
    Runtime RuntimeConfig `mapstructure:"runtime"`
    API     APIConfig     `mapstructure:"api"`
    Redis   RedisConfig   `mapstructure:"redis"`
}
viper.SetEnvPrefix("AETHER")
viper.AutomaticEnv()
```

**Dependencies:** None
**Blocks:** Containerization, multi-environment

---

### P1-SCALE-003: Fix Rate Limiter Precision Issues
**Priority:** P1 - Required for High Throughput
**Severity:** MEDIUM (Enforcement Failure)
**Effort:** 3 days
**File:** `internal/ratelimit/tokenbucket.go:120-214`

**Description:**
Floating-point time causes precision loss at high rates (>1000 req/s). 20% variance at 10K req/s.

**Acceptance Criteria:**
- [ ] Use integer nanoseconds instead of float seconds
- [ ] Calculate tokens with nano precision
- [ ] Track burst debt
- [ ] Implement AllowN (currently stubbed)
- [ ] Unit tests verify precision
- [ ] Load test: 10K req/s, verify ±1% accuracy

**Technical Details:**
```lua
local now_nano = tonumber(ARGV[3])
local elapsed_nano = now_nano - last_refill_nano
local tokens_to_add = (elapsed_nano * rate) / 1000000000
```

**Dependencies:** None
**Blocks:** High-throughput deployments

---

## 🟢 PHASE 5: Observability & Operations (Weeks 9-10)

### P1-OPS-001: Implement Comprehensive Health Checks
**Priority:** P1 - Required for K8s
**Severity:** CRITICAL (Operations)
**Effort:** 3 days
**File:** `internal/api/server.go:125-126`

**Description:**
/health endpoint not implemented. Kubernetes will route traffic to unhealthy instances.

**Acceptance Criteria:**
- [ ] Check Redis connectivity (2s timeout)
- [ ] Check PostgreSQL connectivity (2s timeout)
- [ ] Check Kafka broker health (2s timeout)
- [ ] Check etcd cluster health (2s timeout)
- [ ] Check disk space (>10% free)
- [ ] Return 200 if healthy, 503 if unhealthy
- [ ] JSON response with component status
- [ ] Separate /readiness and /liveness

**Technical Details:**
```go
type HealthCheck struct {
    Name   string
    Status string  // "healthy", "degraded", "unhealthy"
    Error  string
}
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request)
```

**Dependencies:** None
**Blocks:** Kubernetes deployment

---

### P1-OPS-002: Implement Graceful Shutdown
**Priority:** P1 - Required for Production
**Severity:** HIGH (Data Loss)
**Effort:** 2 days
**File:** `cmd/aether/main.go:119-126`

**Description:**
Shutdown abruptly terminates in-flight requests. No drain period for Kubernetes pod termination.

**Acceptance Criteria:**
- [ ] Handle SIGTERM/SIGINT signals
- [ ] Stop accepting new requests
- [ ] Wait for in-flight requests (30s timeout)
- [ ] Drain period for K8s (10s)
- [ ] Stop background workers gracefully
- [ ] Close connections cleanly
- [ ] Log shutdown progress
- [ ] Integration test verifies clean shutdown

**Technical Details:**
```go
shutdownCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
server.Shutdown(shutdownCtx)
time.Sleep(10 * time.Second) // K8s drain
runtime.Shutdown(shutdownCtx)
```

**Dependencies:** None
**Blocks:** Production deployment

---

### P1-OBS-003: Implement Prometheus Metrics Export
**Priority:** P1 - Required for Monitoring
**Severity:** MEDIUM
**Effort:** 1 week
**File:** `internal/observability/metrics.go`

**Description:**
Metrics collected but not exported. No /metrics endpoint for Prometheus scraping.

**Acceptance Criteria:**
- [ ] Add /metrics HTTP endpoint
- [ ] Export all collected metrics
- [ ] Add missing critical metrics:
  - scheduler_queue_depth
  - scheduler_placement_latency_seconds
  - redis_lock_wait_seconds
  - vm_creation_failures_total
  - api_request_duration_seconds
- [ ] Use histogram for latencies
- [ ] Add proper labels (endpoint, tenant, status)
- [ ] Documentation for all metrics

**Technical Details:**
```go
import "github.com/prometheus/client_golang/prometheus/promhttp"
s.router.Handle("/metrics", promhttp.Handler())
```

**Dependencies:** None
**Blocks:** Production monitoring

---

### P1-OBS-004: Complete Distributed Tracing Implementation
**Priority:** P1 - Required for Debugging
**Severity:** MEDIUM
**Effort:** 1 week
**Files:** Multiple

**Description:**
OpenTelemetry initialized but spans not created. No trace context propagation.

**Acceptance Criteria:**
- [ ] Create spans for all major operations
- [ ] Propagate trace context across services
- [ ] Add span attributes (tenant_id, agent_id)
- [ ] Add events for key milestones
- [ ] Configure sampling (100% dev, 1% prod)
- [ ] Integration with Jaeger
- [ ] Documentation for adding traces

**Technical Details:**
```go
ctx, span := tracer.Start(ctx, "scheduler.ScheduleAgent")
defer span.End()
span.SetAttributes(
    attribute.String("tenant_id", tenantID),
    attribute.String("agent_id", agentID),
)
```

**Dependencies:** None
**Blocks:** Production debugging

---

### P1-OPS-005: Add Retry Logic Framework
**Priority:** P1 - Required for Reliability
**Severity:** HIGH
**Effort:** 2 weeks
**Files:** Multiple

**Description:**
No retry logic on transient failures. Network blips cause permanent failures.

**Acceptance Criteria:**
- [ ] Implement generic retry wrapper
- [ ] Exponential backoff with jitter
- [ ] Configurable max retries
- [ ] Classify errors (retryable vs permanent)
- [ ] Circuit breaker integration
- [ ] Apply to:
  - Scheduler placement
  - Redis operations
  - Kafka publish
  - Database queries
- [ ] Unit tests verify retry behavior
- [ ] Chaos test: inject random failures

**Technical Details:**
```go
func WithRetry(ctx context.Context, op func() error, policy RetryPolicy) error {
    backoff := policy.InitialBackoff
    for attempt := 0; attempt < policy.MaxRetries; attempt++ {
        if err := op(); err == nil { return nil }
        if !policy.IsRetryable(err) { return err }
        sleep(backoff)
        backoff = min(backoff * 2, policy.MaxBackoff)
    }
}
```

**Dependencies:** None
**Blocks:** Production reliability

---

## 🟣 PHASE 6: Infrastructure Security (Weeks 11-12)

### P1-INFRA-001: Add Terraform State Backend
**Priority:** P1 - BLOCKER for Team Collaboration
**Severity:** CRITICAL (Data Loss)
**Effort:** 2 days
**Files:** `deployments/terraform/aws/main.tf`, `deployments/terraform/gcp/main.tf`

**Description:**
State stored locally risks data loss. No state locking allows concurrent modification corruption.

**Acceptance Criteria:**
- [ ] Create S3 bucket for state (AWS)
- [ ] Create GCS bucket for state (GCP)
- [ ] Enable versioning on buckets
- [ ] Enable encryption (KMS)
- [ ] Create DynamoDB table for locking (AWS)
- [ ] Configure backend in terraform blocks
- [ ] Migrate existing state
- [ ] Block public access
- [ ] Documentation for state management

**Dependencies:** None
**Blocks:** Team collaboration, CI/CD

---

### P1-INFRA-002: Remove Secrets from Terraform Variables
**Priority:** P1 - BLOCKER for Security
**Severity:** CRITICAL (Credential Exposure)
**Effort:** 1 day
**Files:** `deployments/terraform/aws/variables.tf`, `deployments/terraform/gcp/variables.tf`

**Description:**
Passwords passed as variables end up in state files and Git history.

**Acceptance Criteria:**
- [ ] Use random_password resource
- [ ] Generate passwords in Terraform
- [ ] Remove password variables
- [ ] Verify no secrets in tfvars files
- [ ] Verify no secrets in state file (values generated)
- [ ] Documentation for retrieving passwords

**Technical Details:**
```hcl
resource "random_password" "postgres" {
  length  = 32
  special = true
}
resource "aws_db_instance" "aether" {
  password = random_password.postgres.result
}
```

**Dependencies:** P1-INFRA-001 (state backend)
**Blocks:** None

---

### P1-INFRA-003: Enable CloudTrail and VPC Flow Logs
**Priority:** P1 - Required for Compliance
**Severity:** HIGH (Audit/Security)
**Effort:** 3 days
**File:** `deployments/terraform/aws/main.tf`

**Description:**
No audit trail for API calls. No network traffic visibility for security investigations.

**Acceptance Criteria:**
- [ ] Create CloudTrail trail
- [ ] Create S3 bucket for CloudTrail logs
- [ ] Enable multi-region trail
- [ ] Enable insights (API call rate)
- [ ] Create VPC Flow Logs
- [ ] Send to CloudWatch Logs (30 day retention)
- [ ] IAM role for flow logs
- [ ] Verify logs being written

**Dependencies:** P1-INFRA-001 (state backend)
**Blocks:** SOC 2 compliance

---

### P1-INFRA-004: Add Security Scanning to CI/CD
**Priority:** P1 - Required for Security
**Severity:** HIGH
**Effort:** 2 days
**File:** `.github/workflows/ci.yml` (create new)

**Description:**
No vulnerability scanning. Vulnerable dependencies and IaC misconfigurations in production.

**Acceptance Criteria:**
- [ ] Add Trivy for dependency scanning
- [ ] Add tfsec for Terraform scanning
- [ ] Add Checkov for IaC best practices
- [ ] Add Gitleaks for secret scanning
- [ ] Upload results to GitHub Security
- [ ] Fail build on critical vulnerabilities
- [ ] Documentation for security workflow

**Dependencies:** None
**Blocks:** Production security

---

### P1-INFRA-005: Harden Docker Compose Configuration
**Priority:** P1 - Required for Security
**Severity:** MEDIUM
**Effort:** 1 day
**File:** `deployments/docker/docker-compose.dev.yml`

**Description:**
Multiple security issues: no resource limits, running as root, no network isolation.

**Acceptance Criteria:**
- [ ] Add resource limits (CPU, memory)
- [ ] Run as non-root user
- [ ] Enable read-only root filesystem
- [ ] Add security-opt: no-new-privileges
- [ ] Use secrets for passwords
- [ ] Create isolated network
- [ ] Add health checks
- [ ] Documentation for .env.dev setup

**Dependencies:** P0-SEC-006 (.env file)
**Blocks:** None

---

## 🟤 PHASE 7: Test Coverage (Weeks 13-15)

### P2-TEST-001: VM Lifecycle Error Path Tests
**Priority:** P2 - Quality Improvement
**Severity:** MEDIUM
**Effort:** 1 week
**File:** `internal/runtime/vm/lifecycle_test.go`

**Description:**
Only 3.3% coverage. Critical error paths completely untested.

**Acceptance Criteria:**
- [ ] Test: Firecracker binary not found
- [ ] Test: Socket path already exists
- [ ] Test: Stop timeout forces kill
- [ ] Test: Kill fails, handle gracefully
- [ ] Test: Destroy cleans up tap devices
- [ ] Test: Tap device name collision
- [ ] Test: Concurrent Start calls
- [ ] Test: Wait for ready timeout
- [ ] Test: Cleanup on failure
- [ ] Achieve 80%+ coverage

**Dependencies:** P0-LEAK-001, P0-LEAK-002
**Blocks:** None

---

### P2-TEST-002: Scheduler Concurrency Tests
**Priority:** P2 - Quality Improvement
**Severity:** MEDIUM
**Effort:** 1 week
**File:** `internal/scheduler/scheduler_test.go`

**Description:**
No concurrency tests. Race conditions in queue and allocation undetected.

**Acceptance Criteria:**
- [ ] Test: Concurrent enqueue/dequeue
- [ ] Test: Remove during dequeue
- [ ] Test: Priority ordering under load
- [ ] Test: Concurrent schedule/unschedule
- [ ] Test: Unregister node with agents
- [ ] Test: Event channel overflow
- [ ] Run with -race flag
- [ ] Load test: 1000 concurrent operations

**Dependencies:** P0-RACE-001, P0-RACE-002
**Blocks:** None

---

### P2-TEST-003: Rate Limiter Failure Tests
**Priority:** P2 - Quality Improvement
**Severity:** MEDIUM
**Effort:** 3 days
**File:** `internal/ratelimit/tokenbucket_test.go`

**Description:**
9.2% coverage. Redis failures and edge cases untested.

**Acceptance Criteria:**
- [ ] Test: Redis connection lost
- [ ] Test: Lua script errors
- [ ] Test: Clock skew (negative elapsed)
- [ ] Test: Token overflow
- [ ] Test: Concurrent requests (exactly N allowed)
- [ ] Test: Multi-layer enforcement order
- [ ] Achieve 80%+ coverage

**Dependencies:** P1-SCALE-003 (precision fix)
**Blocks:** None

---

### P2-TEST-004: Backup Corruption Detection Tests
**Priority:** P2 - Quality Improvement
**Severity:** MEDIUM
**Effort:** 3 days
**File:** `internal/backup/backup_test.go`

**Description:**
22.4% coverage. Corruption detection and failure scenarios untested.

**Acceptance Criteria:**
- [ ] Test: PostgreSQL connection lost mid-backup
- [ ] Test: Disk full during backup
- [ ] Test: Redis BGSAVE timeout
- [ ] Test: Concurrent backups
- [ ] Test: Verify detects corrupted backup
- [ ] Test: Partial restore fails gracefully
- [ ] Test: Cleanup during backup
- [ ] Achieve 70%+ coverage

**Dependencies:** None
**Blocks:** None

---

### P2-TEST-005: HA Failover Scenario Tests
**Priority:** P2 - Quality Improvement
**Severity:** MEDIUM
**Effort:** 1 week
**File:** `internal/ha/failover_test.go`

**Description:**
1.6% coverage. Split-brain and failover scenarios untested.

**Acceptance Criteria:**
- [ ] Test: Split-brain (only one leader)
- [ ] Test: Max failovers exceeded
- [ ] Test: Health check timeout
- [ ] Test: False positive (threshold not met)
- [ ] Test: State replication lag
- [ ] Test: Leader session expired
- [ ] Test: Watch disconnect/reconnect
- [ ] Achieve 70%+ coverage

**Dependencies:** None
**Blocks:** None

---

### P2-TEST-006: Auth Token Edge Case Tests
**Priority:** P2 - Quality Improvement
**Severity:** MEDIUM
**Effort:** 2 days
**File:** `internal/auth/jwt_test.go`

**Description:**
Only happy path tested. Expiration and validation edge cases missing.

**Acceptance Criteria:**
- [ ] Test: Expired token
- [ ] Test: Token not yet valid (nbf)
- [ ] Test: Clock skew
- [ ] Test: Wrong issuer
- [ ] Test: Wrong signing method
- [ ] Test: Malformed token
- [ ] Achieve 80%+ coverage

**Dependencies:** P0-SEC-001 (auth implementation)
**Blocks:** None

---

### P2-TEST-007: Integration Tests for Critical Paths
**Priority:** P2 - Quality Improvement
**Severity:** MEDIUM
**Effort:** 1 week
**Files:** `tests/integration/`

**Description:**
No end-to-end integration tests. Component interactions untested.

**Acceptance Criteria:**
- [ ] Test: Create agent → schedule → start → logs → stop → delete
- [ ] Test: Rate limit enforcement across API
- [ ] Test: Tenant isolation across operations
- [ ] Test: Failover preserves state
- [ ] Test: Backup → restore → verify
- [ ] Test: Checkpoint → crash → recover
- [ ] Run against real dependencies (Docker Compose)
- [ ] CI runs integration tests

**Dependencies:** All P0 tickets
**Blocks:** None

---

## 📊 SUMMARY

### By Priority

| Priority | Count | Total Effort |
|----------|-------|--------------|
| P0 | 27 | 7.5 weeks |
| P1 | 15 | 5.5 weeks |
| P2 | 7 | 5.0 weeks |
| **TOTAL** | **49** | **18 weeks** |

### By Phase

| Phase | Tickets | Effort | Must Complete Before |
|-------|---------|--------|---------------------|
| 1. Critical Security | 7 | 1 week | Any deployment |
| 2. Data Integrity | 5 | 2 weeks | Production |
| 3. Resource Management | 7 | 1 week | Stability |
| 4. Scalability | 3 | 4 weeks | >1K agents |
| 5. Observability | 5 | 2 weeks | Operations |
| 6. Infrastructure | 5 | 2 weeks | Compliance |
| 7. Test Coverage | 7 | 5 weeks | Quality assurance |

### By Category

| Category | Tickets | Effort |
|----------|---------|--------|
| Security | 9 | 2 weeks |
| Database/State | 8 | 3 weeks |
| Resource Leaks | 4 | 0.5 weeks |
| Race Conditions | 3 | 1 week |
| Scalability | 5 | 5 weeks |
| Operations | 6 | 3 weeks |
| Infrastructure | 7 | 2 weeks |
| Testing | 7 | 5 weeks |

---

## 🚀 QUICK START - First Week

### Day 1-2: Authentication & Tenant Isolation
- [ ] P0-SEC-001: JWT authentication
- [ ] P0-SEC-002: Tenant isolation

### Day 3: Injection Vulnerabilities
- [ ] P0-SEC-003: SQL injection
- [ ] P0-SEC-004: Command injection

### Day 4-5: Secrets & Input Validation
- [ ] P0-SEC-005: Vault integration
- [ ] P0-SEC-006: Remove hardcoded credentials
- [ ] P0-SEC-007: Input validation

**Outcome:** No P0 security vulnerabilities remain

---

## 📝 NOTES

- All tickets include specific file locations and line numbers
- Acceptance criteria are testable and measurable
- Dependencies clearly marked
- Effort estimates assume 1 engineer (adjust for team size)
- P0 tickets MUST be completed before production
- P1 tickets required for production scale/operations
- P2 tickets improve quality and reduce technical debt

**Last Updated:** 2026-02-01
