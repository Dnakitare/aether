# Phase 1: Foundation - Architecture

## Overview

Phase 1 establishes the core foundation of the Aether runtime with basic VM lifecycle management, agent abstraction, and observability.

## Components

### 1. VM Management (`internal/runtime/vm/`)

The VM management layer provides abstraction over Firecracker microVMs.

#### Key Files:
- `lifecycle.go` - VM creation, start, stop, destroy operations
- `config.go` - VM configuration and conversion from agent config
- `network.go` - Network interface (tap device) setup

#### Responsibilities:
- Manage Firecracker VM lifecycle
- Configure VM resources (CPU, memory)
- Set up network interfaces
- Handle VM process management

#### Key Types:
```go
type Manager struct {
    logger         *slog.Logger
    firecrackerBin string
    workspaceDir   string
    kernelImage    string
    rootfsImage    string
}

type VM struct {
    ID      string
    Config  VMConfig
    cmd     *exec.Cmd
    manager *Manager
}
```

### 2. Agent Management (`internal/runtime/agent/`)

The agent layer provides high-level abstraction for AI agents running in VMs.

#### Key Files:
- `agent.go` - Agent lifecycle and state management
- `metrics.go` - Metrics collection from VMs
- `health.go` - Health checking
- `logs.go` - Log streaming

#### Responsibilities:
- Manage agent lifecycle (start, stop, destroy)
- Collect and expose metrics
- Provide health checks
- Stream logs from agents

#### Key Types:
```go
type Agent struct {
    mu               sync.RWMutex
    info             api.AgentInfo
    logger           *slog.Logger
    vm               VM
    stopChan         chan struct{}
    metricsCollector *MetricsCollector
}
```

### 3. Runtime (`internal/runtime/`)

The main runtime orchestrates VM and agent management.

#### Responsibilities:
- Implement the `api.Runtime` interface
- Manage agent registry
- Coordinate VM creation and agent startup
- Handle graceful shutdown

#### Key Operations:
- `CreateAgent()` - Create VM and agent
- `StartAgent()` - Start agent in VM
- `StopAgent()` - Gracefully stop agent
- `DestroyAgent()` - Clean up all resources
- `ListAgents()` - Query agents (with tenant filtering)

### 4. Observability (`internal/observability/`)

Structured logging foundation for the runtime.

#### Features:
- Context-aware logging with `slog`
- Configurable log levels (debug, info, warn, error)
- Environment-based formatting (JSON for prod, text for dev)
- Service metadata injection

#### Future (Phase 4):
- OpenTelemetry integration for distributed tracing
- Metrics collection with Prometheus
- Log aggregation with Loki

### 5. API Types (`pkg/api/`)

Public API types and interfaces.

#### Key Types:
- `AgentID`, `TenantID` - Type-safe identifiers
- `AgentStatus` - Lifecycle states
- `ResourceLimits` - CPU, memory constraints
- `AgentConfig` - Agent configuration
- `AgentInfo` - Runtime agent information
- `Runtime` - Main runtime interface

### 6. CLI (`cmd/aether/`)

Command-line interface for interacting with the runtime.

#### Commands:
- `aether daemon` - Start runtime daemon
- `aether agent create` - Create and start agent
- `aether agent list` - List all agents
- `aether agent logs <id>` - Stream agent logs
- `aether agent stop <id>` - Stop agent
- `aether agent destroy <id>` - Destroy agent
- `aether agent health <id>` - Check agent health

## Data Flow

### Agent Creation

```
CLI
  └─> Runtime.CreateAgent()
        ├─> VMManager.Create()
        │     ├─> Create workspace directory
        │     ├─> Setup network (tap device)
        │     └─> Return VM instance
        └─> Create Agent wrapper
              └─> Store in agent registry
```

### Agent Startup

```
CLI
  └─> Runtime.StartAgent()
        └─> Agent.Start()
              ├─> VM.Start()
              │     ├─> Write Firecracker config
              │     ├─> Launch Firecracker process
              │     └─> Wait for ready (socket exists)
              ├─> Update agent status
              └─> Start metrics collection
```

### Agent Shutdown

```
CLI
  └─> Runtime.StopAgent()
        └─> Agent.Stop()
              ├─> Stop metrics collector
              ├─> VM.Stop()
              │     ├─> Send SIGTERM
              │     ├─> Wait with timeout
              │     └─> Force kill if needed
              └─> Update agent status
```

## Resource Management

### VM Workspace Structure

```
/var/lib/aether/
  ├── <agent-id>/
  │   ├── firecracker.sock    # API socket
  │   ├── vm.log               # VM logs
  │   ├── metrics.fifo         # Metrics FIFO
  │   └── config.json          # Firecracker config
```

### Resource Limits

Per-agent resource limits:
- CPU: vCPU count (default: 1)
- Memory: MB (default: 512)
- Disk: MB (optional)
- Network bandwidth: Mbps (optional)

## Security Model (Phase 1)

Basic security features implemented:

1. **Process Isolation**: Each agent runs in its own Firecracker microVM
2. **Network Isolation**: Separate tap devices per agent
3. **Resource Limits**: CPU and memory constraints enforced by Firecracker
4. **Workspace Isolation**: Separate directories per agent

## Testing Strategy

### Unit Tests
- API types validation
- VM configuration conversion
- Observability setup

### Integration Tests (Future)
- Full agent lifecycle
- Concurrent agent management
- Resource cleanup

### Test Coverage Target
- Phase 1: 80%+ coverage
- Critical paths: 100% coverage

## Known Limitations

Phase 1 focuses on core functionality. The following are deferred to later phases:

- **Multi-tenancy**: Basic support (tenant ID tracking), no enforcement
- **Authentication**: No auth in Phase 1
- **Metrics Collection**: Placeholder implementation
- **Log Aggregation**: File-based only
- **Auto-scaling**: Not implemented
- **State Persistence**: In-memory only
- **High Availability**: Single instance

## Next Steps (Phase 2)

Phase 2 will add:

1. **Scheduler**: Resource-aware agent placement
2. **Auto-scaling**: Metrics-based scaling
3. **HTTP API**: REST endpoints with OpenAPI spec
4. **Authentication**: JWT-based auth
5. **State Persistence**: Redis integration

## Configuration

### Runtime Configuration

```go
type Config struct {
    VMManagerConfig vm.ManagerConfig
    DefaultResources api.ResourceLimits
    WorkspaceDir string
}
```

### VM Manager Configuration

```go
type ManagerConfig struct {
    FirecrackerBinary string
    WorkspaceDir      string
    KernelImage       string
    RootFSImage       string
}
```

### Observability Configuration

```go
type Config struct {
    LogLevel       LogLevel
    ServiceName    string
    ServiceVersion string
    Environment    string
    EnableTracing  bool
}
```

## Performance Characteristics

### VM Startup Time
- Target: <100ms (Firecracker design goal)
- Current: ~100-500ms (including network setup)

### Memory Overhead
- Firecracker: ~5MB per VM
- Agent wrapper: ~1-2MB
- Total: ~6-7MB overhead per agent

### CPU Overhead
- Minimal when idle
- Metrics collection: ~0.1% CPU per agent

## Debugging

### Log Locations
- Runtime logs: stdout (structured JSON/text)
- VM logs: `<workspace>/<agent-id>/vm.log`

### Common Issues
1. **Firecracker not found**: Check `FirecrackerBinary` path
2. **Permission denied on tap devices**: Requires `CAP_NET_ADMIN` or root
3. **VM fails to start**: Check kernel/rootfs image paths

### Debug Mode
```bash
./bin/aether --log-level=debug daemon
```

## Metrics (Phase 1 - Placeholder)

Current metrics collection returns mock data. Phase 4 will implement:
- CPU usage percentage
- Memory usage (MB)
- Network RX/TX bytes
- Agent count by status
