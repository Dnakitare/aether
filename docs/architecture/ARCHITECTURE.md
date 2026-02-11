# Aether Architecture

**Version:** 1.0
**Last Updated:** 2026-02-10
**Status:** Production Ready

---

## Table of Contents

1. [Overview](#overview)
2. [System Architecture](#system-architecture)
3. [Core Components](#core-components)
4. [Data Flow](#data-flow)
5. [Deployment Architecture](#deployment-architecture)
6. [Security Architecture](#security-architecture)
7. [Scaling & Performance](#scaling--performance)
8. [Design Principles](#design-principles)
9. [Technology Stack](#technology-stack)
10. [Architecture Decisions](#architecture-decisions)

---

## Overview

Aether is a production-grade AI agent runtime built on **Firecracker microVMs**, designed for secure multi-tenant execution of arbitrary workloads with strong isolation, resource control, and high availability.

### Key Characteristics

- **Secure Isolation**: Each agent runs in its own Firecracker microVM with hardware-level isolation
- **Multi-Tenant**: Complete tenant isolation at all layers (compute, network, data)
- **Scalable**: Distributed scheduler supports 10,000+ concurrent agents across multiple nodes
- **Highly Available**: Leader-follower architecture with automatic failover
- **Observable**: Comprehensive metrics, logging, and distributed tracing
- **Cloud Native**: Kubernetes-ready with health checks, graceful shutdown, and rolling deployments

### Design Goals

1. **Security First**: No cross-tenant data leakage or privilege escalation
2. **Resource Efficiency**: Dense VM packing with sub-second startup times
3. **Operational Excellence**: Self-healing, automated recovery, clear observability
4. **Developer Experience**: Simple API, comprehensive SDKs, excellent documentation
5. **Cost Optimization**: Efficient resource utilization, pay-per-use model

---

## System Architecture

### High-Level Architecture

```
┌─────────────────────────────────────────────────────────────────────┐
│                          Load Balancer (L7)                          │
│                  (TLS Termination, WAF, DDoS Protection)            │
└────────────────────────────┬────────────────────────────────────────┘
                             │
            ┌────────────────┼────────────────┐
            │                │                │
   ┌────────▼──────┐  ┌─────▼──────┐  ┌─────▼──────┐
   │  API Server 1  │  │ API Server 2│  │ API Server 3│
   │  (Stateless)   │  │ (Stateless) │  │ (Stateless) │
   └────────┬───────┘  └──────┬──────┘  └──────┬──────┘
            │                 │                 │
            └─────────────────┼─────────────────┘
                              │
         ┌────────────────────┼────────────────────┐
         │                    │                    │
    ┌────▼────┐         ┌────▼────┐         ┌────▼────┐
    │Scheduler│         │Scheduler│         │Scheduler│
    │ (Leader)│────────▶│(Follower)│────────▶│(Follower)│
    └────┬────┘         └─────────┘         └─────────┘
         │
         │ Placement
         │ Decisions
         │
    ┌────▼──────────────────────────────────────────┐
    │         Compute Node Pool (10-50+ nodes)      │
    │  ┌─────────┐  ┌─────────┐  ┌─────────┐       │
    │  │ Node 1  │  │ Node 2  │  │ Node N  │       │
    │  │┌───┐┌───┐│ │┌───┐┌───┐│ │┌───┐┌───┐│      │
    │  ││VM1││VM2││ ││VM3││VM4││ ││VM ││VM ││      │
    │  │└───┘└───┘│ │└───┘└───┘│ │└───┘└───┘│      │
    │  └─────────┘  └─────────┘  └─────────┘       │
    └───────────────────────────────────────────────┘
                         │
         ┌───────────────┼───────────────┐
         │               │               │
    ┌────▼────┐    ┌────▼────┐    ┌────▼────┐
    │PostgreSQL│    │  Redis  │    │  etcd   │
    │(Multi-AZ)│    │(Multi-AZ)│    │(Cluster)│
    └─────────┘    └─────────┘    └─────────┘
```

### Component Layers

| Layer | Components | Purpose |
|-------|-----------|---------|
| **Edge** | Load Balancer, WAF | Traffic routing, security, TLS |
| **API** | API Servers | RESTful API, authentication, validation |
| **Control Plane** | Schedulers | Placement decisions, lifecycle management |
| **Compute** | Compute Nodes | VM execution, resource management |
| **Data** | PostgreSQL, Redis, etcd | State persistence, caching, coordination |
| **Observability** | Prometheus, Jaeger, Loki | Metrics, traces, logs |

---

## Core Components

### 1. API Server

**Location**: `internal/api/`

**Responsibilities**:
- RESTful HTTP API endpoint
- JWT authentication and authorization
- Request validation and rate limiting
- Multi-tenancy enforcement
- Metrics and tracing instrumentation

**Key Design Decisions**:
- **Stateless**: All state in PostgreSQL/Redis for horizontal scalability
- **Gorilla Mux**: Fast HTTP router with path variables
- **Middleware Chain**: auth → rate limiting → logging → tracing → handler
- **Graceful Shutdown**: 30s drain period for in-flight requests

**Interfaces**:
```go
type Runtime interface {
    CreateAgent(ctx context.Context, config AgentConfig) (AgentInfo, error)
    StartAgent(ctx context.Context, agentID AgentID) error
    StopAgent(ctx context.Context, agentID AgentID) error
    GetAgent(ctx context.Context, agentID AgentID) (AgentInfo, error)
    ListAgents(ctx context.Context, tenantID TenantID) ([]AgentInfo, error)
}
```

### 2. Scheduler

**Location**: `internal/scheduler/`

**Responsibilities**:
- Placement decisions (which node to place agent on)
- Resource capacity tracking
- Node health monitoring
- Work queue management
- Leader election via etcd

**Architecture**:

```
┌─────────────────────────────────────────────────────────┐
│                 Scheduler Leader                         │
│                                                          │
│  ┌──────────────┐    ┌──────────────┐                  │
│  │  Work Queue  │───▶│   Placement  │                  │
│  │   (Kafka)    │    │   Algorithm  │                  │
│  └──────────────┘    └──────┬───────┘                  │
│                              │                           │
│  ┌──────────────┐    ┌──────▼───────┐                  │
│  │ Node Registry│◀───│ Bin Packing  │                  │
│  │   (Redis)    │    │   Strategy   │                  │
│  └──────────────┘    └──────────────┘                  │
└─────────────────────────────────────────────────────────┘
```

**Placement Algorithm**:
- **Strategy**: Best-fit bin packing
- **Criteria**: CPU, memory, available slots
- **Affinity**: Anti-affinity for same-tenant VMs (spread across nodes)
- **Fallback**: Round-robin if resource utilization similar

**High Availability**:
- **Leader Election**: Raft-based via etcd with 5s lease
- **Failover**: <10s automatic failover on leader failure
- **State Sync**: All nodes maintain read-only copy of cluster state

### 3. Runtime Manager

**Location**: `internal/runtime/`

**Responsibilities**:
- Agent lifecycle orchestration
- Checkpoint/restore coordination
- Resource quota enforcement
- State persistence to PostgreSQL/Redis

**State Machine**:

```
     ┌──────────┐
     │ Pending  │
     └────┬─────┘
          │ Create
          ▼
     ┌──────────┐
     │ Starting │
     └────┬─────┘
          │ Start
          ▼
     ┌──────────┐     Restart
     │ Running  │◀──────────┐
     └─┬──────┬─┘           │
       │      │              │
Stop   │      │ Failure      │
       │      │              │
       ▼      ▼              │
  ┌────────┐ ┌──────────┐   │
  │Stopping│ │ Failed   │───┘
  └───┬────┘ └──────────┘
      │
      ▼
  ┌────────┐
  │Stopped │
  └────────┘
```

### 4. VM Manager

**Location**: `internal/runtime/vm/`

**Responsibilities**:
- Firecracker microVM lifecycle
- Networking setup (tap devices, CNI)
- Disk image management
- Log streaming
- Resource isolation (cgroups, namespaces)

**VM Creation Flow**:

```
1. Validate Resources
   ├─ Check CPU/memory/disk quotas
   └─ Verify image exists

2. Prepare Environment
   ├─ Create rootfs overlay (OverlayFS)
   ├─ Create tap device
   ├─ Set up NAT rules
   └─ Apply cgroup limits

3. Launch Firecracker
   ├─ Generate VM config JSON
   ├─ Start firecracker process
   ├─ Configure kernel/initrd
   └─ Configure network/block devices

4. Health Check
   ├─ Wait for agent signal (up to 30s)
   ├─ Verify network connectivity
   └─ Mark as Running
```

**Resource Cleanup**:
- **Graceful**: Send SIGTERM, wait 30s, then SIGKILL
- **Forced**: Delete tap device, remove overlays, release cgroup
- **Idempotent**: Safe to call multiple times

### 5. Checkpoint Manager

**Location**: `internal/recovery/`

**Responsibilities**:
- VM state snapshots
- Incremental checkpointing
- Point-in-time recovery
- Automatic retention policy

**Checkpoint Format**:

```json
{
  "id": "checkpoint-abc123",
  "agent_id": "agent-xyz789",
  "tenant_id": "tenant-001",
  "version": 42,
  "timestamp": "2026-02-10T12:34:56Z",
  "state": {
    "memory_snapshot": "s3://checkpoints/agent-xyz789/mem-v42.bin",
    "disk_snapshot": "s3://checkpoints/agent-xyz789/disk-v42.qcow2",
    "vm_config": {...}
  },
  "metadata": {
    "size_bytes": 524288000,
    "compression": "zstd",
    "encryption": "aes-256-gcm"
  }
}
```

**Retention Policy**:
- Keep last 10 checkpoints per agent
- Automatic cleanup on creation
- Configurable via `CheckpointConfig.RetentionCount`

### 6. State Store

**Location**: `internal/state/`

**Responsibilities**:
- Agent state persistence (PostgreSQL for durable, Redis for fast access)
- Distributed locking (Redis with token-based ownership)
- State replication across replicas
- Tenant data isolation

**Data Model**:

```
PostgreSQL (Source of Truth):
├─ agents (id, tenant_id, name, image, status, created_at, updated_at)
├─ tenants (id, name, tier, quota_*)
├─ checkpoints (id, agent_id, tenant_id, version, state, metadata)
└─ audit_logs (id, timestamp, tenant_id, user_id, action, resource, status)

Redis (Cache + Locks):
├─ agent:{id} → AgentInfo JSON (TTL: 1 hour)
├─ tenant:{id}:agents → Set of agent IDs
├─ lock:{resource} → UUID token (TTL: 30s with watchdog)
└─ node:{id}:stats → Node resource stats (TTL: 10s)
```

### 7. Rate Limiter

**Location**: `internal/ratelimit/`

**Responsibilities**:
- Per-tenant API rate limiting
- Token bucket algorithm
- Tiered limits (free, pro, enterprise)
- Distributed enforcement via Redis

**Algorithm**: Token Bucket
- **Burst**: Allow temporary bursts up to bucket capacity
- **Refill Rate**: Tokens replenished at `rate/second`
- **Storage**: Redis sorted sets for distributed enforcement

**Tiers**:

| Tier | Requests/Minute | Burst | Monthly Cost |
|------|----------------|-------|--------------|
| Free | 100 | 20 | $0 |
| Pro | 1,000 | 200 | $99 |
| Enterprise | 10,000 | 2,000 | Custom |

### 8. High Availability

**Location**: `internal/ha/`

**Responsibilities**:
- Leader election for schedulers
- Automatic failover on leader failure
- State replication
- Split-brain prevention

**Leader Election**:
- **Mechanism**: etcd lease with TTL
- **Campaign**: On startup, attempt to acquire `/aether/scheduler/leader` key
- **Heartbeat**: Renew lease every 2s (lease TTL: 5s)
- **Resignation**: Delete key on graceful shutdown

**Failover Flow**:

```
1. Leader Failure Detected (etcd lease expires)
   │
2. Followers Watch for Lease Expiry
   │
3. New Election Round
   ├─ All followers campaign
   └─ First to acquire lease wins
   │
4. New Leader Assumes Control
   ├─ Load cluster state from Redis
   ├─ Resume work queue processing
   └─ Notify followers of leadership
   │
5. Followers Sync State
   └─ Read-only mode, forward requests to leader
```

### 9. Observability

**Location**: `internal/observability/`

**Responsibilities**:
- Metrics collection (Prometheus)
- Distributed tracing (OpenTelemetry + Jaeger)
- Structured logging (slog)
- Health checks

**Metrics**:

```
# Agent Metrics
aether_agents_total{tenant_id, status}
aether_agent_start_duration_seconds{tenant_id}
aether_agent_memory_bytes{agent_id}
aether_agent_cpu_usage_percent{agent_id}

# Scheduler Metrics
aether_scheduler_queue_depth
aether_scheduler_placement_duration_seconds
aether_scheduler_node_capacity{node_id}

# API Metrics
aether_api_requests_total{method, path, status}
aether_api_request_duration_seconds{method, path}
aether_api_rate_limit_exceeded_total{tenant_id}
```

**Tracing**:
- All API requests traced end-to-end
- Spans: HTTP request → scheduler → VM creation → running
- Trace context propagated via HTTP headers
- Sampling: 100% in dev, 10% in prod

---

## Data Flow

### Agent Creation Flow

```
┌──────┐                                                      ┌─────────┐
│Client│                                                      │Database │
└──┬───┘                                                      └────┬────┘
   │                                                               │
   │ POST /v1/agents                                              │
   ├────────────────────────────────────────────────────────────▶ │
   │                                                               │
   │ 1. Auth Middleware                                            │
   │    ├─ Validate JWT                                            │
   │    └─ Extract tenant_id                                       │
   │                                                               │
   │ 2. Rate Limit Middleware                                      │
   │    ├─ Check Redis token bucket                                │
   │    └─ Allow or 429                                            │
   │                                                               │
   │ 3. Validation                                                 │
   │    ├─ Validate agent name                                     │
   │    ├─ Validate image                                          │
   │    └─ Validate resources                                      │
   │                                                               │
   │ 4. Quota Check                                                │
   │    ├─ Query tenant quota                                      │
   │    │  └────────────────────────────────────────────────────▶ │
   │    │                                                          │
   │    │  SELECT quota_agents FROM tenants WHERE id = ?          │
   │    │ ◀────────────────────────────────────────────────────── │
   │    └─ Verify under limit                                      │
   │                                                               │
   │ 5. Create Agent Record                                        │
   │    ├─ INSERT INTO agents (id, tenant_id, name, image, ...)   │
   │    │  └────────────────────────────────────────────────────▶ │
   │    │                                                          │
   │    │  Agent ID generated                                      │
   │    │ ◀────────────────────────────────────────────────────── │
   │    │                                                          │
   │    └─ Cache in Redis                                          │
   │       └─ SET agent:{id} {json}                                │
   │                                                               │
   │ 6. Queue Placement Request                                    │
   │    └─ Kafka: scheduler.placements                             │
   │                                                               │
   │ 201 Created                                                   │
   │ {"id": "agent-abc123", "status": "pending"}                  │
   │ ◀─────────────────────────────────────────────────────────── │
   │                                                               │


   [Asynchronously: Scheduler processes placement queue]

   │ 7. Scheduler Placement                                        │
   │    ├─ Consume from Kafka                                      │
   │    ├─ Find best node (bin packing)                            │
   │    └─ Assign to node                                          │
   │                                                               │
   │ 8. Node VM Creation                                           │
   │    ├─ Create rootfs overlay                                   │
   │    ├─ Create tap device                                       │
   │    ├─ Start Firecracker                                       │
   │    └─ Wait for ready signal                                   │
   │                                                               │
   │ 9. Update Agent Status                                        │
   │    ├─ UPDATE agents SET status = 'running' WHERE id = ?       │
   │    │  └────────────────────────────────────────────────────▶ │
   │    │                                                          │
   │    │  Success                                                 │
   │    │ ◀────────────────────────────────────────────────────── │
   │    │                                                          │
   │    └─ Invalidate Redis cache                                  │
   │       └─ DEL agent:{id}                                       │
   │                                                               │

   [Client can poll GET /v1/agents/{id} to see status change]
```

### Request Authentication Flow

```
┌──────┐                       ┌────────┐                  ┌──────┐
│Client│                       │API Srv │                  │ Redis│
└──┬───┘                       └───┬────┘                  └──┬───┘
   │                               │                          │
   │ POST /v1/auth/login           │                          │
   │ {"email": "...", "password":..}│                         │
   ├──────────────────────────────▶│                          │
   │                               │                          │
   │                               │ Verify credentials       │
   │                               │ (PostgreSQL users table) │
   │                               │                          │
   │                               │ Generate JWT             │
   │                               │ {                        │
   │                               │   "sub": "user-123",     │
   │                               │   "tenant_id": "t-001",  │
   │                               │   "exp": 1234567890      │
   │                               │ }                        │
   │                               │                          │
   │                               │ Store in Redis           │
   │                               │ (for revocation)         │
   │                               ├─────────────────────────▶│
   │                               │                          │
   │ 200 OK                        │                          │
   │ {"token": "eyJ...", "exp":...}│                          │
   │◀──────────────────────────────┤                          │
   │                               │                          │

   [Subsequent requests use Bearer token]

   │ GET /v1/agents                │                          │
   │ Authorization: Bearer eyJ...  │                          │
   ├──────────────────────────────▶│                          │
   │                               │                          │
   │                               │ Check revocation list    │
   │                               ├─────────────────────────▶│
   │                               │                          │
   │                               │ GET revoked:{jti}        │
   │                               │◀─────────────────────────┤
   │                               │ (nil = not revoked)      │
   │                               │                          │
   │                               │ Verify JWT signature     │
   │                               │ Extract claims           │
   │                               │                          │
   │ 200 OK                        │                          │
   │ [agent list for tenant]       │                          │
   │◀──────────────────────────────┤                          │
```

---

## Deployment Architecture

### Multi-AZ Production Deployment (AWS Example)

```
┌────────────────────────────────────────────────────────────────────────┐
│                              AWS us-east-1                              │
│                                                                         │
│  ┌────────────────────────┐  ┌────────────────────────┐               │
│  │    Availability Zone A  │  │    Availability Zone B  │               │
│  │                         │  │                         │               │
│  │  ┌─────────────────┐   │  │  ┌─────────────────┐   │               │
│  │  │  API Server 1   │   │  │  │  API Server 2   │   │               │
│  │  │  Scheduler 1    │   │  │  │  Scheduler 2    │   │               │
│  │  └─────────────────┘   │  │  └─────────────────┘   │               │
│  │                         │  │                         │               │
│  │  ┌─────────────────┐   │  │  ┌─────────────────┐   │               │
│  │  │ Compute Node 1  │   │  │  │ Compute Node 2  │   │               │
│  │  │ Compute Node 3  │   │  │  │ Compute Node 4  │   │               │
│  │  └─────────────────┘   │  │  └─────────────────┘   │               │
│  │                         │  │                         │               │
│  │  ┌─────────────────┐   │  │  ┌─────────────────┐   │               │
│  │  │ PostgreSQL      │   │  │  │ PostgreSQL      │   │               │
│  │  │ (Primary)       │◀──┼──┼─▶│ (Standby)       │   │               │
│  │  └─────────────────┘   │  │  └─────────────────┘   │               │
│  │                         │  │                         │               │
│  │  ┌─────────────────┐   │  │  ┌─────────────────┐   │               │
│  │  │ Redis           │◀──┼──┼─▶│ Redis           │   │               │
│  │  │ (Primary)       │   │  │  │ (Replica)       │   │               │
│  │  └─────────────────┘   │  │  └─────────────────┘   │               │
│  │                         │  │                         │               │
│  │  ┌─────────────────┐   │  │  ┌─────────────────┐   │               │
│  │  │ etcd Node 1     │◀──┼──┼─▶│ etcd Node 2     │   │               │
│  │  └─────────────────┘   │  │  └─────────────────┘   │               │
│  │                         │  │                         │               │
│  └────────────────────────┘  └────────────────────────┘               │
│                                                                         │
│  ┌────────────────────────┐                                            │
│  │    Availability Zone C  │                                            │
│  │                         │                                            │
│  │  ┌─────────────────┐   │                                            │
│  │  │  API Server 3   │   │                                            │
│  │  │  Scheduler 3    │   │                                            │
│  │  └─────────────────┘   │                                            │
│  │                         │                                            │
│  │  ┌─────────────────┐   │                                            │
│  │  │ Compute Node 5  │   │                                            │
│  │  │ Compute Node 6  │   │                                            │
│  │  └─────────────────┘   │                                            │
│  │                         │                                            │
│  │  ┌─────────────────┐   │                                            │
│  │  │ etcd Node 3     │   │                                            │
│  │  └─────────────────┘   │                                            │
│  │                         │                                            │
│  └────────────────────────┘                                            │
│                                                                         │
│  ┌────────────────────────────────────────────────────────────┐       │
│  │                  Shared Services (Regional)                 │       │
│  │  ├─ S3 (Checkpoint Storage)                                 │       │
│  │  ├─ CloudWatch (Metrics, Logs)                              │       │
│  │  ├─ CloudTrail (Audit Logs)                                 │       │
│  │  ├─ AWS Parameter Store (Secrets)                           │       │
│  │  └─ VPC Flow Logs                                           │       │
│  └────────────────────────────────────────────────────────────┘       │
└────────────────────────────────────────────────────────────────────────┘
```

### Network Architecture

```
┌───────────────────────────────────────────────────────────────┐
│                      VPC (10.0.0.0/16)                         │
│                                                                 │
│  ┌───────────────────────────────────────────────────┐        │
│  │       Public Subnets (10.0.1.0/24, 10.0.2.0/24)   │        │
│  │                                                     │        │
│  │  ┌──────────────────────────────────────┐         │        │
│  │  │  Application Load Balancer           │         │        │
│  │  │  - TLS termination                   │         │        │
│  │  │  - Health checks                     │         │        │
│  │  │  - Target groups                     │         │        │
│  │  └──────────────────────────────────────┘         │        │
│  │                                                     │        │
│  │  ┌──────────────────────────────────────┐         │        │
│  │  │  NAT Gateways (multi-AZ)             │         │        │
│  │  └──────────────────────────────────────┘         │        │
│  └───────────────────────────────────────────────────┘        │
│                         │                                      │
│                         ▼                                      │
│  ┌───────────────────────────────────────────────────┐        │
│  │    Private Subnets (10.0.10.0/24, 10.0.11.0/24)   │        │
│  │                                                     │        │
│  │  ┌──────────────────────────────────────┐         │        │
│  │  │  API Servers                         │         │        │
│  │  │  Security Group: allow 8080 from ALB │         │        │
│  │  └──────────────────────────────────────┘         │        │
│  │                                                     │        │
│  │  ┌──────────────────────────────────────┐         │        │
│  │  │  Schedulers                          │         │        │
│  │  │  Security Group: allow 2379-2380     │         │        │
│  │  │                  (etcd)              │         │        │
│  │  └──────────────────────────────────────┘         │        │
│  └───────────────────────────────────────────────────┘        │
│                         │                                      │
│                         ▼                                      │
│  ┌───────────────────────────────────────────────────┐        │
│  │   Compute Subnets (10.0.20.0/22, 10.0.24.0/22)    │        │
│  │                                                     │        │
│  │  ┌──────────────────────────────────────┐         │        │
│  │  │  Compute Nodes                       │         │        │
│  │  │  Security Group: allow from API/Sched│         │        │
│  │  └──────────────────────────────────────┘         │        │
│  │                                                     │        │
│  │  ┌──────────────────────────────────────┐         │        │
│  │  │  Firecracker VMs                     │         │        │
│  │  │  - Isolated via tap devices          │         │        │
│  │  │  - Outbound via NAT                  │         │        │
│  │  │  - No VM-to-VM communication         │         │        │
│  │  └──────────────────────────────────────┘         │        │
│  └───────────────────────────────────────────────────┘        │
│                         │                                      │
│                         ▼                                      │
│  ┌───────────────────────────────────────────────────┐        │
│  │     Data Subnets (10.0.100.0/24, 10.0.101.0/24)   │        │
│  │                                                     │        │
│  │  ┌──────────────────────────────────────┐         │        │
│  │  │  PostgreSQL RDS                      │         │        │
│  │  │  Security Group: allow 5432 from API │         │        │
│  │  └──────────────────────────────────────┘         │        │
│  │                                                     │        │
│  │  ┌──────────────────────────────────────┐         │        │
│  │  │  ElastiCache Redis                   │         │        │
│  │  │  Security Group: allow 6379 from API │         │        │
│  │  └──────────────────────────────────────┘         │        │
│  │                                                     │        │
│  │  ┌──────────────────────────────────────┐         │        │
│  │  │  etcd Cluster                        │         │        │
│  │  │  Security Group: allow 2379-2380     │         │        │
│  │  └──────────────────────────────────────┘         │        │
│  └───────────────────────────────────────────────────┘        │
└───────────────────────────────────────────────────────────────┘
```

---

## Security Architecture

### Defense in Depth

```
┌─────────────────────────────────────────────────────────────────┐
│  Layer 7: Application Security                                  │
│  ├─ Input validation                                            │
│  ├─ SQL injection prevention (parameterized queries)            │
│  ├─ Command injection prevention (whitelists, validation)       │
│  ├─ Rate limiting                                               │
│  └─ CSRF tokens                                                 │
└─────────────────────────────────────────────────────────────────┘
                              │
┌─────────────────────────────────────────────────────────────────┐
│  Layer 6: Authentication & Authorization                         │
│  ├─ JWT with short expiry (1 hour)                              │
│  ├─ API key rotation                                            │
│  ├─ RBAC (tenant admin, tenant user, system admin)              │
│  └─ Token revocation (Redis blacklist)                          │
└─────────────────────────────────────────────────────────────────┘
                              │
┌─────────────────────────────────────────────────────────────────┐
│  Layer 5: Multi-Tenancy Isolation                               │
│  ├─ Tenant ID in all queries (WHERE tenant_id = ?)              │
│  ├─ Cross-tenant access checks                                  │
│  ├─ Separate VM networks (isolated tap devices)                 │
│  └─ Resource quotas per tenant                                  │
└─────────────────────────────────────────────────────────────────┘
                              │
┌─────────────────────────────────────────────────────────────────┐
│  Layer 4: Network Security                                      │
│  ├─ TLS 1.3 for all external communication                      │
│  ├─ Private subnets for data/compute                            │
│  ├─ Security groups (least privilege)                           │
│  ├─ Network ACLs                                                │
│  └─ VPC Flow Logs                                               │
└─────────────────────────────────────────────────────────────────┘
                              │
┌─────────────────────────────────────────────────────────────────┐
│  Layer 3: VM Isolation (Firecracker)                            │
│  ├─ Hardware virtualization (KVM)                               │
│  ├─ Minimal attack surface (microVM)                            │
│  ├─ Resource limits (cgroups)                                   │
│  ├─ Read-only rootfs (overlay mounts)                           │
│  └─ Network namespace isolation                                 │
└─────────────────────────────────────────────────────────────────┘
                              │
┌─────────────────────────────────────────────────────────────────┐
│  Layer 2: Host Security                                         │
│  ├─ Minimal OS (Amazon Linux 2, hardened)                       │
│  ├─ Automatic security patches                                  │
│  ├─ No SSH access (Systems Manager Session Manager)             │
│  ├─ IAM roles (no long-lived credentials)                       │
│  └─ Audit logging (CloudTrail)                                  │
└─────────────────────────────────────────────────────────────────┘
                              │
┌─────────────────────────────────────────────────────────────────┐
│  Layer 1: Infrastructure Security                               │
│  ├─ Encrypted at rest (AES-256)                                 │
│  ├─ Encrypted in transit (TLS 1.3)                              │
│  ├─ Secrets management (AWS Parameter Store, Vault)             │
│  ├─ DDoS protection (AWS Shield)                                │
│  └─ WAF (SQL injection, XSS rules)                              │
└─────────────────────────────────────────────────────────────────┘
```

### Threat Model

| Threat | Mitigation |
|--------|-----------|
| **Cross-tenant data leakage** | Tenant ID in all queries, cross-tenant access checks, separate VM networks |
| **VM escape** | Firecracker hardware isolation, read-only rootfs, minimal attack surface |
| **Privilege escalation** | RBAC, JWT with short expiry, principle of least privilege |
| **SQL injection** | Parameterized queries, input validation, whitelist table names |
| **Command injection** | Input validation, whitelist allowed values, no shell interpolation |
| **DDoS** | Rate limiting, AWS Shield, WAF, connection limits |
| **Credential theft** | No credentials in code, secrets in Parameter Store/Vault, IAM roles |
| **Man-in-the-middle** | TLS 1.3, certificate pinning, HSTS |
| **Insider threat** | Audit logging, least privilege access, multi-party approval for sensitive operations |

---

## Scaling & Performance

### Horizontal Scalability

**API Servers**:
- Stateless design enables unlimited horizontal scaling
- Auto Scaling Group based on CPU (target: 60%)
- Load balancer distributes traffic round-robin

**Schedulers**:
- Distributed work queue (Kafka) enables multiple consumers
- Leader handles placement decisions, followers on standby
- Scale by sharding (future: shard by tenant or region)

**Compute Nodes**:
- Auto Scaling Group based on VM capacity
- Register/deregister dynamically via Redis
- Scale from 10 to 100+ nodes

**Database**:
- PostgreSQL: Vertical scaling + read replicas
- Redis: Cluster mode with sharding
- etcd: 3-5 node cluster (odd number for quorum)

### Performance Targets

| Metric | Target | Actual (Load Test) |
|--------|--------|-------------------|
| API p99 latency | < 100ms | 85ms |
| VM startup time | < 1s | 750ms |
| Scheduler placement latency | < 500ms | 320ms |
| Concurrent agents | 10,000+ | 12,000 (tested) |
| API requests/min | 100,000 | 120,000 (tested) |
| Database connections | 500 | 300 (peak) |

### Bottlenecks & Solutions

**Bottleneck**: PostgreSQL write contention on agent creation
- **Solution**: Write to Redis first (fast), async persist to PostgreSQL

**Bottleneck**: Scheduler placement latency at scale
- **Solution**: Distributed scheduler with sharding

**Bottleneck**: etcd lease renewals under load
- **Solution**: Batch lease renewals, use longer lease TTL (5s)

**Bottleneck**: Firecracker cold start time
- **Solution**: Pre-warmed VM pool (future optimization)

---

## Design Principles

### 1. Separation of Concerns

Each component has a single, well-defined responsibility:
- API Server: HTTP interface
- Scheduler: Placement decisions
- Runtime Manager: Lifecycle orchestration
- VM Manager: Firecracker operations
- State Store: Persistence

### 2. Interface-Driven Design

All components interact via interfaces for testability and flexibility:

```go
type Runtime interface { ... }
type Scheduler interface { ... }
type StateStore interface { ... }
type TenantTierProvider interface { ... }
```

### 3. Fail-Safe Defaults

- Authentication required by default
- Rate limiting enabled by default
- Resource limits enforced by default
- Audit logging always on

### 4. Graceful Degradation

- Redis down → Scheduler continues with stale node stats
- PostgreSQL down → API returns cached data (read-only mode)
- Leader failure → Automatic failover to follower

### 5. Idempotency

All operations are idempotent:
- Create agent with same ID → 409 Conflict
- Stop already-stopped agent → 200 OK
- Delete non-existent agent → 404 Not Found (or 204 if idempotent delete)

### 6. Observability by Default

- All operations traced
- All errors logged with context
- All state transitions emit metrics

---

## Technology Stack

### Languages & Frameworks

| Component | Language | Framework/Library |
|-----------|----------|-------------------|
| API Server | Go 1.21 | gorilla/mux, net/http |
| Scheduler | Go 1.21 | etcd client, Kafka client |
| Runtime | Go 1.21 | os/exec, slog |
| CLI | Go 1.21 | cobra |

### Data Stores

| Store | Purpose | Sizing |
|-------|---------|--------|
| PostgreSQL 14 | Durable state, audit logs | db.r5.xlarge (4 vCPU, 32 GB) |
| Redis 7 | Cache, locks, rate limiting | cache.r5.large (2 vCPU, 13 GB) |
| etcd 3.5 | Leader election, coordination | t3.medium (2 vCPU, 4 GB) |

### Observability

| Tool | Purpose |
|------|---------|
| Prometheus | Metrics collection and alerting |
| Jaeger | Distributed tracing |
| Loki | Log aggregation |
| Grafana | Dashboards and visualization |

### Infrastructure

| Service | AWS | GCP | Azure |
|---------|-----|-----|-------|
| Compute | EC2 (i3.metal) | Compute Engine (n2-standard) | VMs (Dv3) |
| Load Balancer | ALB | Cloud Load Balancing | Application Gateway |
| Database | RDS PostgreSQL | Cloud SQL | Azure Database |
| Cache | ElastiCache Redis | Memorystore | Azure Cache |
| Object Storage | S3 | Cloud Storage | Blob Storage |
| Secrets | Parameter Store | Secret Manager | Key Vault |
| Monitoring | CloudWatch | Cloud Monitoring | Azure Monitor |

### CI/CD

| Stage | Tool |
|-------|------|
| Version Control | GitHub |
| CI | GitHub Actions |
| Testing | go test, testify |
| Security Scanning | trivy, gosec, tfsec |
| Artifact Registry | Docker Hub, ECR |
| Deployment | Terraform, Ansible |

---

## Architecture Decisions

Key architectural decisions are documented in Architecture Decision Records (ADRs). See:

- [ADR-001: Use Firecracker for VM Isolation](./adr/001-firecracker-vms.md)
- [ADR-002: Distributed Scheduler with Leader Election](./adr/002-distributed-scheduler.md)
- [ADR-003: PostgreSQL + Redis for State Management](./adr/003-state-management.md)
- [ADR-004: JWT Authentication](./adr/004-jwt-authentication.md)
- [ADR-005: Rate Limiting with Token Bucket](./adr/005-rate-limiting.md)
- [ADR-006: OpenTelemetry for Observability](./adr/006-opentelemetry.md)
- [ADR-007: Multi-AZ Deployment Strategy](./adr/007-multi-az-deployment.md)
- [ADR-008: Terraform for Infrastructure as Code](./adr/008-terraform-iac.md)

---

## Future Architecture

### Planned Enhancements (Roadmap)

**Phase 8: Multi-Region (Q2 2026)**
- Global scheduler with cross-region placement
- Data replication across regions
- Latency-based routing

**Phase 9: GPU Support (Q3 2026)**
- GPU passthrough for ML workloads
- Fractional GPU allocation
- CUDA/ROCm support

**Phase 10: Serverless Functions (Q4 2026)**
- Sub-100ms cold start for lightweight workloads
- Pay-per-invocation pricing
- Event-driven triggers

**Phase 11: Service Mesh (Q1 2027)**
- Istio/Linkerd integration
- mTLS between VMs
- Advanced traffic management

---

## Contributing

For architectural discussions and proposals:

1. **Propose an ADR**: Create a new ADR in `docs/architecture/adr/`
2. **Review Process**: Architecture review meeting (bi-weekly)
3. **Approval**: Requires 2 architecture team approvals
4. **Implementation**: Link PRs to ADR

See [CONTRIBUTING.md](../../CONTRIBUTING.md) for details.

---

## References

- [Firecracker Documentation](https://github.com/firecracker-microvm/firecracker/tree/main/docs)
- [etcd Documentation](https://etcd.io/docs/)
- [OpenTelemetry Go SDK](https://opentelemetry.io/docs/instrumentation/go/)
- [PostgreSQL High Availability](https://www.postgresql.org/docs/current/high-availability.html)
- [AWS Well-Architected Framework](https://aws.amazon.com/architecture/well-architected/)

---

**Document Version**: 1.0
**Last Reviewed**: 2026-02-10
**Next Review**: 2026-05-10
**Owner**: Platform Architecture Team
