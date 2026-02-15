# Aether - Technical Gaps Analysis

**Date:** February 15, 2026
**Purpose:** Detailed breakdown of what needs to be built/fixed
**Audience:** Developers implementing the missing pieces

---

## Gap Categories

```
🔴 CRITICAL   - Blocks basic functionality
🟡 HIGH       - Needed for production
🟢 MEDIUM     - Improves experience
⚪ LOW        - Nice to have
```

---

## 1. Database Layer Gaps

### 🔴 CRITICAL: No SQL Queries Implemented

**Current State:**
- `/internal/state/state.go` - Interfaces defined ✅
- `/internal/state/redis.go` - Implemented ✅
- `/internal/state/postgres.go` - **STUB** ❌

**What's Missing:**
```go
// File: internal/state/postgres.go
// All functions return "not implemented" error

func (p *PostgresStore) CreateAgent(...) error {
    return fmt.Errorf("not implemented")  // ❌
}
```

**What Needs to Be Built:**

```go
// internal/state/postgres.go

func (p *PostgresStore) CreateAgent(ctx context.Context, config api.AgentConfig) error {
    query := `
        INSERT INTO agents (id, tenant_id, name, image, cpu_count, memory_mb, status, created_at)
        VALUES ($1, $2, $3, $4, $5, $6, $7, NOW())
    `
    _, err := p.db.ExecContext(ctx, query,
        config.ID,
        config.TenantID,
        config.Name,
        config.Image,
        config.Resources.CPUCount,
        config.Resources.MemoryMB,
        api.AgentStatusPending,
    )
    return err
}

func (p *PostgresStore) GetAgent(ctx context.Context, id api.AgentID) (*api.AgentInfo, error) {
    query := `
        SELECT id, tenant_id, name, image, cpu_count, memory_mb, status, created_at, updated_at
        FROM agents
        WHERE id = $1
    `
    var info api.AgentInfo
    err := p.db.QueryRowContext(ctx, query, id).Scan(
        &info.Config.ID,
        &info.Config.TenantID,
        &info.Config.Name,
        &info.Config.Image,
        &info.Config.Resources.CPUCount,
        &info.Config.Resources.MemoryMB,
        &info.Status,
        &info.CreatedAt,
        &info.UpdatedAt,
    )
    if err == sql.ErrNoRows {
        return nil, fmt.Errorf("agent not found: %s", id)
    }
    return &info, err
}

// Similar implementations needed for:
// - ListAgents
// - UpdateAgentStatus
// - DeleteAgent
// - SaveCheckpoint
// - LoadCheckpoint
```

**Tests Needed:**
```go
// internal/state/postgres_test.go

func TestPostgresStore_CreateAgent(t *testing.T)
func TestPostgresStore_GetAgent(t *testing.T)
func TestPostgresStore_ListAgents(t *testing.T)
func TestPostgresStore_UpdateAgentStatus(t *testing.T)
func TestPostgresStore_DeleteAgent(t *testing.T)
func TestPostgresStore_AgentNotFound(t *testing.T)
func TestPostgresStore_DuplicateAgent(t *testing.T)
```

**Effort:** 2-3 days
**Priority:** 🔴 CRITICAL

---

### 🔴 CRITICAL: Runtime Not Using State Store

**Current State:**
```go
// internal/runtime/runtime.go
type Runtime struct {
    logger    *slog.Logger
    vmManager *vm.Manager
    config    Config

    mu     sync.RWMutex
    agents map[api.AgentID]*agent.Agent  // ❌ In-memory only!
}
```

**What Needs to Change:**
```go
type Runtime struct {
    logger    *slog.Logger
    vmManager *vm.Manager
    store     state.Store  // ✅ Add state store
    config    Config

    mu     sync.RWMutex
    agents map[api.AgentID]*agent.Agent  // Keep for in-memory cache
}

func (r *Runtime) CreateAgent(ctx context.Context, config api.AgentConfig) error {
    // ... existing VM creation code ...

    // ✅ ADD: Persist to database
    if err := r.store.CreateAgent(ctx, config); err != nil {
        // Clean up VM
        vmInstance.Destroy(ctx)
        return fmt.Errorf("failed to persist agent: %w", err)
    }

    // ... rest of function
}

func (r *Runtime) StartAgent(ctx context.Context, id api.AgentID) error {
    // ✅ ADD: Load from database if not in memory
    agentInstance, err := r.getAgent(id)
    if err != nil {
        // Try loading from database
        info, err := r.store.GetAgent(ctx, id)
        if err != nil {
            return err
        }
        // Recreate agent from stored config
        if err := r.recreateAgent(ctx, info); err != nil {
            return err
        }
        agentInstance, _ = r.getAgent(id)
    }

    // ... rest of function
}
```

**Effort:** 1 day
**Priority:** 🔴 CRITICAL

---

### 🟡 HIGH: Migrations Command Missing

**Current State:**
- `migrations/001_initial_schema.up.sql` exists ✅
- CLI has no `migrate` command ❌
- README says `./aether migrate up` ❌

**What Needs to Be Built:**
```go
// cmd/aether/migrate.go

import "github.com/golang-migrate/migrate/v4"

var migrateCmd = &cobra.Command{
    Use:   "migrate",
    Short: "Database migrations",
}

var migrateUpCmd = &cobra.Command{
    Use:   "up",
    Short: "Apply all pending migrations",
    RunE: func(cmd *cobra.Command, args []string) error {
        m, err := migrate.New(
            "file://migrations",
            cfg.Database.ConnectionString(),
        )
        if err != nil {
            return err
        }
        return m.Up()
    },
}

var migrateDownCmd = &cobra.Command{
    Use:   "down",
    Short: "Rollback last migration",
    RunE: func(cmd *cobra.Command, args []string) error {
        m, err := migrate.New(
            "file://migrations",
            cfg.Database.ConnectionString(),
        )
        if err != nil {
            return err
        }
        return m.Steps(-1)
    },
}

func init() {
    migrateCmd.AddCommand(migrateUpCmd)
    migrateCmd.AddCommand(migrateDownCmd)
    rootCmd.AddCommand(migrateCmd)
}
```

**Effort:** 4 hours
**Priority:** 🟡 HIGH

---

## 2. Firecracker Integration Gaps

### 🔴 CRITICAL: Firecracker Not Actually Called

**Current State:**
```go
// internal/runtime/vm/lifecycle.go

func (v *VM) writeFirecrackerConfig(path string) error {
    // This is a simplified version - in production, use proper JSON marshaling
    config := fmt.Sprintf(`{...}`, ...)  // ❌ String formatting!
    return os.WriteFile(path, []byte(config), 0644)
}
```

**Problems:**
1. Config uses string formatting (error-prone)
2. Network interfaces not included
3. Firecracker process starts but config is incomplete
4. No API calls to Firecracker after boot

**What Needs to Be Built:**

```go
// internal/runtime/vm/firecracker_config.go

type FirecrackerConfig struct {
    BootSource       BootSource         `json:"boot-source"`
    Drives           []Drive            `json:"drives"`
    MachineConfig    MachineConfig      `json:"machine-config"`
    NetworkInterfaces []NetworkInterface `json:"network-interfaces"`
}

type BootSource struct {
    KernelImagePath string `json:"kernel_image_path"`
    BootArgs        string `json:"boot_args"`
}

type Drive struct {
    DriveID       string `json:"drive_id"`
    PathOnHost    string `json:"path_on_host"`
    IsRootDevice  bool   `json:"is_root_device"`
    IsReadOnly    bool   `json:"is_read_only"`
}

type MachineConfig struct {
    VcpuCount  int `json:"vcpu_count"`
    MemSizeMib int `json:"mem_size_mib"`
}

type NetworkInterface struct {
    IfaceID     string `json:"iface_id"`
    GuestMAC    string `json:"guest_mac"`
    HostDevName string `json:"host_dev_name"`
}

func (v *VM) writeFirecrackerConfig(path string) error {
    config := FirecrackerConfig{
        BootSource: BootSource{
            KernelImagePath: v.Config.KernelImagePath,
            BootArgs:        v.Config.BootArgs,
        },
        Drives: []Drive{{
            DriveID:      "rootfs",
            PathOnHost:   v.Config.RootfsPath,
            IsRootDevice: true,
            IsReadOnly:   false,
        }},
        MachineConfig: MachineConfig{
            VcpuCount:  v.Config.CPUCount,
            MemSizeMib: v.Config.MemoryMB,
        },
        NetworkInterfaces: buildNetworkInterfaces(v.Config),
    }

    data, err := json.MarshalIndent(config, "", "  ")
    if err != nil {
        return err
    }

    return os.WriteFile(path, data, 0644)
}

func buildNetworkInterfaces(config VMConfig) []NetworkInterface {
    interfaces := make([]NetworkInterface, len(config.NetworkInterfaces))
    for i, netif := range config.NetworkInterfaces {
        interfaces[i] = NetworkInterface{
            IfaceID:     netif.ID,
            GuestMAC:    netif.MAC,
            HostDevName: netif.HostDevName,
        }
    }
    return interfaces
}
```

**Effort:** 1 day
**Priority:** 🔴 CRITICAL

---

### 🔴 CRITICAL: No Communication with Running VM

**What's Missing:**
- No vsock setup for host-guest communication
- No serial console access
- No way to execute commands in guest
- No way to copy files to/from guest

**What Needs to Be Built:**
```go
// internal/runtime/vm/guest_exec.go

// Execute command in guest via vsock
func (v *VM) Exec(ctx context.Context, cmd string) (string, error) {
    // Option 1: Use vsock to connect to guest agent
    conn, err := vsock.Dial(v.Config.CID, GUEST_AGENT_PORT)
    if err != nil {
        return "", err
    }
    defer conn.Close()

    // Send command
    if err := json.NewEncoder(conn).Encode(ExecRequest{Cmd: cmd}); err != nil {
        return "", err
    }

    // Read response
    var resp ExecResponse
    if err := json.NewDecoder(conn).Decode(&resp); err != nil {
        return "", err
    }

    return resp.Output, resp.Error
}

// Option 2: Use Firecracker serial console
func (v *VM) ExecSerial(ctx context.Context, cmd string) (string, error) {
    // Not recommended - vsock is better
}
```

**Note:** This requires guest agent running in VM (needs to be added to rootfs image)

**Effort:** 2-3 days
**Priority:** 🔴 CRITICAL

---

## 3. API Server Gaps

### 🔴 CRITICAL: API Server Not Started

**Current State:**
```go
// cmd/aether/server.go line 142-143

// TODO: Create and start HTTP API server
// For now, just wait for shutdown signal
```

**What Needs to Be Added:**
```go
// cmd/aether/server.go

// After creating distributed components...

// Create runtime
rt, err := runtime.New(logger, runtime.Config{...})
if err != nil {
    return err
}

// Create HTTP API server
apiServer := api.New(logger, api.Config{
    Address:      cfg.Server.Address,
    ReadTimeout:  cfg.Server.ReadTimeout,
    WriteTimeout: cfg.Server.WriteTimeout,
    EnableCORS:   cfg.Server.EnableCORS,
    EnableAuth:   cfg.Server.EnableAuth,
    TracingConfig: &observability.TracerConfig{
        Enabled:    cfg.Observability.TracingEnabled,
        Endpoint:   cfg.Observability.JaegerEndpoint,
        SampleRate: cfg.Observability.TracingSampleRate,
    },
}, rt, sched, sc, qm, jwtMgr)

// Register health checks
apiServer.RegisterHealthCheck("postgres", postgresHealthCheck)
apiServer.RegisterHealthCheck("redis", redisHealthCheck)
if cfg.Scheduler.Mode == "distributed" {
    apiServer.RegisterHealthCheck("etcd", etcdHealthCheck)
    apiServer.RegisterHealthCheck("kafka", kafkaHealthCheck)
}

// Start API server
if err := apiServer.Start(ctx); err != nil {
    return fmt.Errorf("failed to start API server: %w", err)
}

// Register shutdown hook
defer func() {
    shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()
    if err := apiServer.Stop(shutdownCtx); err != nil {
        logger.ErrorContext(shutdownCtx, "error stopping API server", "error", err)
    }
}()

logger.InfoContext(ctx, "Aether server started successfully")
```

**Effort:** 2 hours
**Priority:** 🔴 CRITICAL

---

### 🟡 HIGH: Missing Authentication Endpoints

**What Exists:**
- JWT manager implemented ✅
- Auth middleware implemented ✅
- API key auth implemented ✅

**What's Missing:**
- No `/v1/auth/login` endpoint ❌
- No `/v1/auth/register` endpoint ❌
- No way to get JWT token ❌

**What Needs to Be Added:**
```go
// internal/api/auth_handlers.go

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
    var req struct {
        Email    string `json:"email"`
        Password string `json:"password"`
    }

    if err := s.parseJSON(r, &req); err != nil {
        s.respondError(w, http.StatusBadRequest, "invalid request")
        return
    }

    // Validate credentials (needs user table)
    user, err := s.userStore.GetByEmail(r.Context(), req.Email)
    if err != nil {
        s.respondError(w, http.StatusUnauthorized, "invalid credentials")
        return
    }

    if !user.CheckPassword(req.Password) {
        s.respondError(w, http.StatusUnauthorized, "invalid credentials")
        return
    }

    // Generate JWT token
    token, err := s.jwtManager.GenerateToken(user.ID, user.TenantID, user.Role)
    if err != nil {
        s.respondError(w, http.StatusInternalServerError, "failed to generate token")
        return
    }

    s.respondJSON(w, http.StatusOK, map[string]string{
        "token": token,
        "type":  "Bearer",
    })
}

// Register in setupRoutes()
v1.HandleFunc("/auth/login", s.handleLogin).Methods("POST")
```

**Note:** Requires user table in database (not in current migrations)

**Effort:** 1 day (including user table migration)
**Priority:** 🟡 HIGH

---

### 🟡 HIGH: Agent Exec Endpoint Missing

**What's Documented:**
```bash
# From README
curl -X POST http://localhost:8080/v1/agents/{id}/exec \
  -H "Authorization: Bearer $TOKEN" \
  -d '{"command": "python -c \"print(123)\""}'
```

**What Exists:**
- Nothing ❌

**What Needs to Be Added:**
```go
// internal/api/handlers.go

func (s *Server) handleExecAgent(w http.ResponseWriter, r *http.Request) {
    vars := mux.Vars(r)
    agentID := api.AgentID(vars["id"])

    var req struct {
        Command string `json:"command"`
        Timeout int    `json:"timeout"` // seconds
    }

    if err := s.parseJSON(r, &req); err != nil {
        s.respondError(w, http.StatusBadRequest, "invalid request")
        return
    }

    // Get tenant from JWT
    claims, ok := auth.GetClaims(r.Context())
    if !ok {
        s.respondError(w, http.StatusUnauthorized, "unauthorized")
        return
    }

    // Execute command
    ctx := r.Context()
    if req.Timeout > 0 {
        var cancel context.CancelFunc
        ctx, cancel = context.WithTimeout(ctx, time.Duration(req.Timeout)*time.Second)
        defer cancel()
    }

    output, err := s.runtime.ExecAgent(ctx, agentID, req.Command)
    if err != nil {
        s.respondError(w, http.StatusInternalServerError, err.Error())
        return
    }

    s.respondJSON(w, http.StatusOK, map[string]string{
        "output": output,
    })
}

// Register in setupRoutes()
v1.HandleFunc("/agents/{id}/exec", s.handleExecAgent).Methods("POST")
```

**Also needs runtime support:**
```go
// internal/runtime/runtime.go

func (r *Runtime) ExecAgent(ctx context.Context, id api.AgentID, cmd string) (string, error) {
    agentInstance, err := r.getAgent(id)
    if err != nil {
        return "", err
    }

    // Delegate to VM
    return agentInstance.Exec(ctx, cmd)
}
```

**Effort:** 1-2 days (depends on vsock implementation)
**Priority:** 🟡 HIGH

---

## 4. Testing Gaps

### 🔴 CRITICAL: One Test Failing

**Current State:**
```
FAIL: TestAPIKeyPrefixFormat/key_has_correct_format
Expected: "TOFn_kQk"
Actual: "TOFn"
```

**Root Cause:** API key generation changed but test not updated

**Fix:**
```go
// internal/auth/apikey_edgecase_test.go

// Find the test and update expectations
// OR fix the API key generation to match expected format
```

**Effort:** 30 minutes
**Priority:** 🔴 CRITICAL

---

### 🟡 HIGH: No API Handler Tests

**Current State:**
```
internal/api                        0.0%      ❌ No Tests
```

**What Needs to Be Added:**
```go
// internal/api/handlers_test.go

func TestHandleListAgents(t *testing.T)
func TestHandleCreateAgent(t *testing.T)
func TestHandleGetAgent(t *testing.T)
func TestHandleDeleteAgent(t *testing.T)
func TestHandleGetAgentLogs(t *testing.T)
func TestHandleGetAgentHealth(t *testing.T)

// Auth tests
func TestHandleLogin(t *testing.T)
func TestHandleLoginInvalidCredentials(t *testing.T)
func TestAuthMiddleware(t *testing.T)
```

**Effort:** 2-3 days
**Priority:** 🟡 HIGH

---

### 🟡 HIGH: No End-to-End Test

**What's Missing:**
- No test that creates → starts → execs → stops → destroys agent
- No test with real database
- No test with real Firecracker

**What Needs to Be Added:**
```go
// tests/integration/e2e_test.go

func TestCompleteAgentLifecycle(t *testing.T) {
    if testing.Short() {
        t.Skip("E2E test requires infrastructure")
    }

    // Setup infrastructure
    db := setupPostgres(t)
    defer db.Close()

    // Start server
    server := startAetherServer(t, db)
    defer server.Shutdown()

    // Login
    token := login(t, server, "admin@example.com", "password")

    // Create agent
    agentID := createAgent(t, server, token, CreateAgentRequest{
        Name:  "test-agent",
        Image: "python:3.11-slim",
        Resources: Resources{
            CPUCount: 1,
            MemoryMB: 512,
        },
    })

    // Wait for agent to be running
    waitForAgentStatus(t, server, token, agentID, "Running", 30*time.Second)

    // Execute command
    output := execInAgent(t, server, token, agentID, "python -c 'print(123)'")
    assert.Contains(t, output, "123")

    // Get logs
    logs := getAgentLogs(t, server, token, agentID)
    assert.Contains(t, logs, "123")

    // Stop agent
    stopAgent(t, server, token, agentID)
    waitForAgentStatus(t, server, token, agentID, "Stopped", 10*time.Second)

    // Restart server (test persistence)
    server.Shutdown()
    server = startAetherServer(t, db)

    // Verify agent still exists
    agent := getAgent(t, server, token, agentID)
    assert.Equal(t, "Stopped", agent.Status)

    // Delete agent
    deleteAgent(t, server, token, agentID)

    // Verify deleted
    _, err := getAgent(t, server, token, agentID)
    assert.Error(t, err)
}
```

**Effort:** 2-3 days
**Priority:** 🟡 HIGH

---

## 5. Infrastructure Gaps

### 🟡 HIGH: Terraform Doesn't Deploy App

**What Exists:**
- VPC, subnets, security groups ✅
- RDS PostgreSQL ✅
- ElastiCache Redis ✅
- CloudTrail ✅

**What's Missing:**
- ECS task definitions ❌
- ECS service ❌
- Application Load Balancer ❌
- Auto Scaling Groups ❌
- CloudWatch dashboards ❌

**What Needs to Be Added:**
```hcl
# deployments/terraform/aws/ecs.tf

resource "aws_ecs_cluster" "aether" {
  name = "aether-${var.environment}"

  setting {
    name  = "containerInsights"
    value = "enabled"
  }
}

resource "aws_ecs_task_definition" "aether_server" {
  family                   = "aether-server"
  requires_compatibilities = ["FARGATE"]
  network_mode            = "awsvpc"
  cpu                     = "1024"
  memory                  = "2048"

  container_definitions = jsonencode([{
    name  = "aether"
    image = "${var.docker_image}:${var.docker_tag}"

    portMappings = [{
      containerPort = 8080
      protocol      = "tcp"
    }]

    environment = [
      { name = "AETHER_DATABASE_HOST", value = aws_db_instance.postgres.address },
      { name = "AETHER_REDIS_ADDRESS", value = aws_elasticache_cluster.redis.cache_nodes[0].address },
      # ... more config
    ]

    secrets = [
      { name = "AETHER_DATABASE_PASSWORD", valueFrom = aws_secretsmanager_secret.db_password.arn },
    ]

    logConfiguration = {
      logDriver = "awslogs"
      options = {
        "awslogs-group"         = aws_cloudwatch_log_group.aether.name
        "awslogs-region"        = var.region
        "awslogs-stream-prefix" = "aether"
      }
    }
  }])
}

resource "aws_ecs_service" "aether_server" {
  name            = "aether-server"
  cluster         = aws_ecs_cluster.aether.id
  task_definition = aws_ecs_task_definition.aether_server.arn
  desired_count   = 2

  load_balancer {
    target_group_arn = aws_lb_target_group.aether.arn
    container_name   = "aether"
    container_port   = 8080
  }

  network_configuration {
    subnets          = aws_subnet.private[*].id
    security_groups  = [aws_security_group.aether_server.id]
    assign_public_ip = false
  }
}

# ALB, target groups, etc.
```

**Effort:** 1 week
**Priority:** 🟡 HIGH

---

### 🟢 MEDIUM: No CI/CD Pipeline

**What's Missing:**
- GitHub Actions workflow ❌
- Automated testing ❌
- Docker image building ❌
- Deployment automation ❌

**What Needs to Be Added:**
```yaml
# .github/workflows/ci.yml

name: CI

on:
  push:
    branches: [ main ]
  pull_request:
    branches: [ main ]

jobs:
  test:
    runs-on: ubuntu-latest

    services:
      postgres:
        image: postgres:15
        env:
          POSTGRES_DB: aether_test
          POSTGRES_USER: aether
          POSTGRES_PASSWORD: password
        ports:
          - 5432:5432

      redis:
        image: redis:7
        ports:
          - 6379:6379

    steps:
      - uses: actions/checkout@v3

      - name: Set up Go
        uses: actions/setup-go@v4
        with:
          go-version: '1.21'

      - name: Run tests
        run: make test

      - name: Run linters
        run: make lint

      - name: Upload coverage
        uses: codecov/codecov-action@v3

  build:
    needs: test
    runs-on: ubuntu-latest

    steps:
      - uses: actions/checkout@v3

      - name: Build Docker image
        run: docker build -t aether:${{ github.sha }} .

      - name: Push to registry
        if: github.ref == 'refs/heads/main'
        run: |
          docker tag aether:${{ github.sha }} ${{ secrets.DOCKER_REGISTRY }}/aether:latest
          docker push ${{ secrets.DOCKER_REGISTRY }}/aether:latest
```

**Effort:** 2 days
**Priority:** 🟢 MEDIUM

---

## 6. Documentation Gaps

### 🟡 HIGH: README Overpromises

**What README Claims:**
- "Production-ready" ❌
- "Battle-tested" ❌
- "12,000 concurrent agents tested" ❌ (never tested)
- "99.9% uptime SLA" ❌ (never measured)
- `./aether migrate up` ❌ (command doesn't exist)

**What Needs to Be Fixed:**

Add prominent alpha/beta notice:
```markdown
## ⚠️ Status: Alpha

Aether is currently in **alpha** status. Core features work but it's not production-ready.

### ✅ What Works
- Local scheduler
- Authentication & authorization
- Rate limiting
- Configuration system
- Basic VM management

### 🚧 In Progress
- Database persistence
- Firecracker execution
- Distributed scheduling
- Production deployment

### ❌ Not Ready
- High availability
- Load balancing
- Multi-node deployment
- Production workloads

**Use at your own risk. Not recommended for production.**
```

Update benchmarks:
```markdown
## Performance (Projected)

These are design goals, not measured results:
- VM startup: <1s (target)
- API latency: <100ms (target)
- Concurrent agents: 10,000+ (design capacity)

Actual performance testing in progress.
```

**Effort:** 2 hours
**Priority:** 🟡 HIGH

---

## Summary: Critical Path to MVP

### Must Fix (2 weeks)

1. ✅ Fix failing test (30 min)
2. ✅ Database queries (2 days)
3. ✅ Wire runtime to database (1 day)
4. ✅ Firecracker config (1 day)
5. ✅ Start API server (2 hours)
6. ✅ Auth endpoints (1 day)
7. ✅ Exec endpoint + vsock (2-3 days)
8. ✅ End-to-end test (2 days)
9. ✅ Update docs (2 hours)
10. ✅ Migrations command (4 hours)

**Total: 10-12 days of focused work**

### Should Fix (4 weeks)

11. ✅ API handler tests (3 days)
12. ✅ Terraform deployment (1 week)
13. ✅ CI/CD pipeline (2 days)
14. ✅ Guest agent for exec (3 days)
15. ✅ Performance testing (3 days)

**Total: 3-4 weeks additional**

---

## Conclusion

**The good news:** Most components are 80-90% complete.

**The challenge:** Integration work to connect them.

**The timeline:** 2 weeks to working demo, 6 weeks to beta quality.

**The approach:** Fix critical blockers first, then iterate.

---

**Document Version:** 1.0
**Last Updated:** February 15, 2026
**Next Review:** After critical fixes complete
