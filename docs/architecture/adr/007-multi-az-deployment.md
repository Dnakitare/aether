# ADR-007: Multi-AZ Deployment Strategy

**Status**: Accepted
**Date**: 2025-12-10
**Decision Makers**: Platform Architecture Team, SRE Team
**Technical Story**: High availability with 99.9% uptime SLA

---

## Context

Aether must meet enterprise SLA requirements:

1. **Uptime**: 99.9% availability (43 minutes downtime/month max)
2. **RPO**: Recovery Point Objective <5 minutes (max data loss)
3. **RTO**: Recovery Time Objective <15 minutes (max downtime)
4. **Disaster Recovery**: Survive availability zone failure
5. **Zero-Downtime Deployments**: Rolling updates without user impact
6. **Cost Efficiency**: Balance redundancy with infrastructure costs

### Failure Scenarios

| Failure | Likelihood | Impact | Mitigation Required |
|---------|-----------|--------|-------------------|
| Single instance crash | High (monthly) | Low | Auto-restart, health checks |
| AZ-wide outage | Low (yearly) | Critical | Multi-AZ deployment |
| Region-wide outage | Very Low (rare) | Critical | Multi-region (future) |
| Database failure | Low (yearly) | Critical | Multi-AZ replica, automated failover |
| Network partition | Medium | High | Split-brain prevention, consensus |

### Alternatives Considered

| Strategy | Pros | Cons | Decision |
|----------|------|------|----------|
| **Single AZ** | Cheapest, simplest | No HA, violates SLA | ❌ Rejected |
| **Active-Passive (2 AZ)** | Simple failover | Wasted capacity, slower failover | ❌ Rejected |
| **Active-Active (2 AZ)** | Better utilization | More complex | ⚠️ Partial (stateless components) |
| **Active-Active (3 AZ)** | Best availability, quorum-based consensus | Highest cost | ✅ **Accepted** |
| **Multi-Region** | Survives region outage | Very expensive, complex replication | ⏳ Future (Phase 8) |

---

## Decision

**We will deploy Aether across 3 availability zones in an active-active configuration with automated failover.**

### Architecture

```
┌────────────────────────────────────────────────────────────────────┐
│                         AWS Region (us-east-1)                      │
│                                                                     │
│  ┌────────────────────┐  ┌────────────────────┐  ┌───────────────┐│
│  │  Availability       │  │  Availability       │  │  Availability ││
│  │  Zone A (us-east-1a)│  │  Zone B (us-east-1b)│  │  Zone C      ││
│  │                     │  │                     │  │  (us-east-1c) ││
│  │                     │  │                     │  │               ││
│  │  ┌──────────────┐  │  │  ┌──────────────┐  │  │  ┌──────────┐││
│  │  │ API Server 1 │  │  │  │ API Server 2 │  │  │  │ API Srv 3│││
│  │  │ (Active)     │  │  │  │ (Active)     │  │  │  │ (Active) │││
│  │  └──────────────┘  │  │  └──────────────┘  │  │  └──────────┘││
│  │                     │  │                     │  │               ││
│  │  ┌──────────────┐  │  │  ┌──────────────┐  │  │  ┌──────────┐││
│  │  │ Scheduler 1  │  │  │  │ Scheduler 2  │  │  │  │Scheduler ││││
│  │  │ (Leader)     │  │  │  │ (Follower)   │  │  │  │(Follower)│││
│  │  └──────────────┘  │  │  └──────────────┘  │  │  └──────────┘││
│  │                     │  │                     │  │               ││
│  │  ┌──────────────┐  │  │  ┌──────────────┐  │  │  ┌──────────┐││
│  │  │Compute Node 1│  │  │  │Compute Node 3│  │  │  │Compute   │││
│  │  │Compute Node 2│  │  │  │Compute Node 4│  │  │  │Node 5,6  │││
│  │  └──────────────┘  │  │  └──────────────┘  │  │  └──────────┘││
│  │                     │  │                     │  │               ││
│  │  ┌──────────────┐  │  │  ┌──────────────┐  │  │               ││
│  │  │ PostgreSQL   │  │  │  │ PostgreSQL   │  │  │               ││
│  │  │ (Primary)    │◀─┼──┼─▶│ (Standby)    │  │  │               ││
│  │  └──────────────┘  │  │  └──────────────┘  │  │               ││
│  │                     │  │                     │  │               ││
│  │  ┌──────────────┐  │  │  ┌──────────────┐  │  │               ││
│  │  │ Redis        │  │  │  │ Redis        │  │  │               ││
│  │  │ (Primary)    │◀─┼──┼─▶│ (Replica)    │  │  │               ││
│  │  └──────────────┘  │  │  └──────────────┘  │  │               ││
│  │                     │  │                     │  │               ││
│  │  ┌──────────────┐  │  │  ┌──────────────┐  │  │  ┌──────────┐││
│  │  │ etcd Node 1  │◀─┼──┼─▶│ etcd Node 2  │◀─┼──┼─▶│etcd N 3  │││
│  │  └──────────────┘  │  │  └──────────────┘  │  │  └──────────┘││
│  │                     │  │                     │  │               ││
│  └────────────────────┘  └────────────────────┘  └───────────────┘│
│                                                                     │
│  ┌──────────────────────────────────────────────────────────────┐ │
│  │           Multi-AZ Services (Regional)                        │ │
│  │  ├─ S3 (Checkpoint Storage)                                   │ │
│  │  ├─ ELB/ALB (Load Balancer with health checks)               │ │
│  │  ├─ CloudWatch (Metrics, Logs, Alerts)                       │ │
│  │  └─ Route 53 (DNS with health checks)                        │ │
│  └──────────────────────────────────────────────────────────────┘ │
└────────────────────────────────────────────────────────────────────┘
```

### Component Placement

| Component | Deployment Strategy | Reason |
|-----------|-------------------|--------|
| **API Servers** | 3 instances (1 per AZ), active-active | Stateless, horizontally scalable |
| **Schedulers** | 3 instances (1 per AZ), leader-follower | Consensus requires odd number for quorum |
| **Compute Nodes** | 6+ instances (2+ per AZ), active-active | VM workloads, spread for anti-affinity |
| **PostgreSQL** | 1 primary + 1 standby (different AZs) | Synchronous replication, auto-failover |
| **Redis** | 1 primary + 1 replica (different AZs) | Async replication, auto-failover (Sentinel/Elasticache) |
| **etcd** | 3 instances (1 per AZ), quorum | Consensus requires odd number |
| **Load Balancer** | Multi-AZ ALB | Automatic failover by AWS |
| **S3** | Multi-AZ by default | Managed by AWS |

---

## Implementation Details

### 1. API Server High Availability

```yaml
# Auto Scaling Group
resource "aws_autoscaling_group" "api" {
  name = "aether-api"

  # Spread across 3 AZs
  vpc_zone_identifier = [
    aws_subnet.private_a.id,
    aws_subnet.private_b.id,
    aws_subnet.private_c.id,
  ]

  min_size = 3
  max_size = 10
  desired_capacity = 3

  # Health checks
  health_check_type = "ELB"
  health_check_grace_period = 300

  # Target groups
  target_group_arns = [aws_lb_target_group.api.arn]

  # Instance distribution
  mixed_instances_policy {
    instances_distribution {
      on_demand_base_capacity = 3
      on_demand_percentage_above_base_capacity = 0
      spot_allocation_strategy = "capacity-optimized"
    }
  }
}

# Application Load Balancer
resource "aws_lb" "api" {
  name = "aether-api"
  load_balancer_type = "application"

  # Multi-AZ
  subnets = [
    aws_subnet.public_a.id,
    aws_subnet.public_b.id,
    aws_subnet.public_c.id,
  ]

  # Health checks
  enable_cross_zone_load_balancing = true
  enable_deletion_protection = true
}

# Target group with health checks
resource "aws_lb_target_group" "api" {
  name = "aether-api"
  port = 8080
  protocol = "HTTP"
  vpc_id = aws_vpc.main.id

  health_check {
    path = "/health"
    interval = 30
    timeout = 5
    healthy_threshold = 2
    unhealthy_threshold = 2
    matcher = "200"
  }

  # Deregistration delay for graceful shutdown
  deregistration_delay = 30
}
```

### 2. PostgreSQL High Availability

```yaml
# RDS PostgreSQL with Multi-AZ
resource "aws_db_instance" "postgres" {
  identifier = "aether-postgres"
  engine = "postgres"
  engine_version = "14.9"

  # Instance type
  instance_class = "db.r5.xlarge"

  # Multi-AZ deployment
  multi_az = true
  availability_zone = "us-east-1a"  # Primary AZ

  # Storage
  allocated_storage = 100
  storage_type = "gp3"
  storage_encrypted = true

  # Backups
  backup_retention_period = 7
  backup_window = "03:00-04:00"
  maintenance_window = "sun:04:00-sun:05:00"

  # Monitoring
  enabled_cloudwatch_logs_exports = ["postgresql"]
  monitoring_interval = 60
  monitoring_role_arn = aws_iam_role.rds_monitoring.arn

  # Automatic failover
  auto_minor_version_upgrade = true
}
```

**Failover Process**:
1. RDS detects primary failure (30-120 seconds)
2. Automatic promotion of standby to primary
3. DNS updated to point to new primary (CNAME)
4. Application reconnects (connection pool retries)
5. Total downtime: <2 minutes

### 3. Redis High Availability

```yaml
# ElastiCache Redis with Auto-Failover
resource "aws_elasticache_replication_group" "redis" {
  replication_group_id = "aether-redis"
  replication_group_description = "Aether Redis cluster"

  # Engine
  engine = "redis"
  engine_version = "7.0"
  node_type = "cache.r5.large"

  # Multi-AZ with auto-failover
  automatic_failover_enabled = true
  multi_az_enabled = true

  # Replica configuration
  num_cache_clusters = 2  # 1 primary + 1 replica

  # Subnet group (spans multiple AZs)
  subnet_group_name = aws_elasticache_subnet_group.redis.name

  # Maintenance
  maintenance_window = "sun:05:00-sun:06:00"
  snapshot_window = "03:00-04:00"
  snapshot_retention_limit = 5

  # Monitoring
  notification_topic_arn = aws_sns_topic.redis_alerts.arn
}
```

**Failover Process**:
1. Redis Sentinel detects primary failure (10-30 seconds)
2. Automatic promotion of replica to primary
3. DNS updated (elasticache endpoint)
4. Application reconnects
5. Total downtime: <1 minute

### 4. etcd High Availability

```yaml
# 3-node etcd cluster (1 per AZ)
resource "aws_instance" "etcd" {
  count = 3
  ami = "ami-etcd-hardened"
  instance_type = "t3.medium"

  # Spread across AZs
  availability_zone = element(["us-east-1a", "us-east-1b", "us-east-1c"], count.index)
  subnet_id = element([
    aws_subnet.private_a.id,
    aws_subnet.private_b.id,
    aws_subnet.private_c.id,
  ], count.index)

  # User data for etcd cluster setup
  user_data = templatefile("${path.module}/etcd-init.sh", {
    cluster_name = "aether-etcd"
    cluster_state = "new"
    initial_cluster = "etcd-0=https://10.0.10.10:2380,etcd-1=https://10.0.11.10:2380,etcd-2=https://10.0.12.10:2380"
  })
}
```

**Quorum Requirements**:
- 3 nodes total
- Quorum: 2 nodes (majority)
- Tolerate: 1 node failure
- If 2 nodes fail: Read-only mode (no leader election)

---

## Consequences

### Positive

✅ **High Availability**: Survives AZ failure (<15 min RTO)
✅ **Zero-Downtime Deployments**: Rolling updates across AZs
✅ **Automatic Failover**: PostgreSQL, Redis, etcd all auto-failover
✅ **Data Durability**: Synchronous replication (PostgreSQL)
✅ **SLA Compliance**: 99.9% uptime achievable

### Negative

❌ **Cost**: 3x infrastructure (3 AZs vs 1)
❌ **Complexity**: Consensus protocols, replication, failover logic
❌ **Cross-AZ Latency**: 1-2ms between AZs (acceptable)
❌ **Data Transfer Costs**: Cross-AZ data transfer (~$0.01/GB)

### Neutral

⚖️ **RTO**: 15 minutes (acceptable for SLA, could optimize further)
⚖️ **RPO**: 5 minutes (synchronous replication = 0, but backups every 5 min)

---

## Failure Scenarios & Recovery

### Scenario 1: Single API Server Failure

**Detection**: Health check fails (30 seconds)
**Impact**: 0% (other API servers handle load)
**Recovery**: Auto Scaling Group launches replacement (5 minutes)
**User Impact**: None

### Scenario 2: Single AZ Failure

**Detection**: Multiple health checks fail (1-2 minutes)
**Impact**:
- 1 API server down (33% capacity loss, acceptable)
- Scheduler failover (8-10 seconds)
- Compute nodes in failed AZ offline (agents fail)
- PostgreSQL/Redis failover (1-2 minutes)

**Recovery**:
1. Load balancer stops routing to failed AZ
2. Scheduler follower elected leader
3. PostgreSQL standby promoted to primary
4. Redis replica promoted to primary
5. New instances launched in healthy AZs

**User Impact**: 1-2 minutes degraded performance, agents in failed AZ lost

### Scenario 3: PostgreSQL Primary Failure

**Detection**: RDS detects failure (30-120 seconds)
**Impact**: All writes blocked
**Recovery**:
1. RDS promotes standby to primary (automatic)
2. DNS updated to new primary
3. Application reconnects

**User Impact**: 1-2 minutes downtime

### Scenario 4: Network Partition (Split Brain)

**Detection**: etcd cluster loses quorum
**Impact**: Scheduler cannot elect leader
**Recovery**:
1. Partition heals (network restored)
2. etcd re-establishes quorum
3. New leader elected

**Mitigation**: etcd leases prevent split-brain (only one leader possible)

---

## Cost Analysis

### Infrastructure Costs (Monthly, Medium Deployment)

| Component | Single AZ | Multi-AZ (3 AZ) | Increase |
|-----------|-----------|----------------|----------|
| API Servers (3x c5.xlarge) | $306 | $306 | 0% (same count) |
| Schedulers (3x t3.medium) | $38 | $38 | 0% (same count) |
| Compute Nodes (6x i3.metal) | $14,976 | $14,976 | 0% (same count) |
| PostgreSQL (db.r5.xlarge) | $365 | $730 | +100% (standby) |
| Redis (cache.r5.large) | $182 | $364 | +100% (replica) |
| etcd (3x t3.medium) | $38 | $38 | 0% (same count) |
| Load Balancer (ALB) | $23 | $23 | 0% (multi-AZ included) |
| Data Transfer (cross-AZ) | $0 | $50 | +$50/month |
| **Total** | **$15,928** | **$16,525** | **+3.7%** |

**Cost per 9 of Availability**:
- Single AZ: $15,928/month → 99% uptime (7 hours downtime/month)
- Multi-AZ: $16,525/month → 99.9% uptime (43 min downtime/month)
- **Cost per additional 9**: $597/month (worth it for SLA)

---

## Risks & Mitigations

| Risk | Likelihood | Impact | Mitigation |
|------|-----------|--------|-----------|
| Entire region outage | Very Low | Critical | Multi-region (Phase 8), backups in different region |
| Simultaneous AZ failures | Very Low | Critical | Impossible to mitigate (accept risk) |
| Slow failover (>15 min) | Low | High | Automated testing, runbooks, alerting |
| Data loss during failover | Low | Critical | Synchronous replication (PostgreSQL), frequent backups |
| Cost overruns | Medium | Low | Monitor costs, optimize instance sizes, reserved instances |

---

## Validation

### Chaos Engineering Tests

✅ **Random API Server Termination**
- Impact: None (load distributed to others)
- Recovery: 5 minutes (new instance launched)

✅ **AZ Failure Simulation** (shut down all instances in AZ-A)
- Impact: 1-2 minutes degraded performance
- Recovery: 10 minutes (full capacity restored)

✅ **PostgreSQL Failover Test**
- Downtime: 1 minute 45 seconds
- Data loss: 0 transactions (synchronous replication)

✅ **Network Partition Test** (isolate etcd node)
- Scheduler failover: 8 seconds
- No split-brain observed

---

## SLA Calculation

### Uptime Math

**Target**: 99.9% uptime = 43 minutes downtime/month

**Component Availability** (AWS SLAs):
- EC2: 99.99% (4.5 min/month)
- RDS Multi-AZ: 99.95% (22 min/month)
- ELB: 99.99% (4.5 min/month)
- S3: 99.99% (4.5 min/month)

**Aether Availability** (composite):
- Single AZ: 99.0% (7 hours/month) - ❌ Fails SLA
- Multi-AZ (3 AZ): 99.92% (35 min/month) - ✅ Exceeds SLA

**Downtime Budget**: 43 minutes/month
- Planned maintenance: 15 minutes (rolling deployments)
- Unplanned outages: 28 minutes remaining
- Actual (load tests): 20 minutes/month average

---

## Future Enhancements

### Phase 8: Multi-Region (Q2 2026)

- Active-passive: us-east-1 (primary), us-west-2 (backup)
- Cross-region replication: PostgreSQL logical replication, S3 CRR
- Failover: Route 53 health checks + automated DNS failover
- RTO: <15 minutes, RPO: <1 minute

### Phase 9: Active-Active Multi-Region (Q4 2026)

- Both regions serve traffic (geo-routing)
- CRDT-based conflict resolution
- Eventual consistency
- RTO: 0 (no failover needed)

---

## References

- [AWS Well-Architected: Reliability Pillar](https://docs.aws.amazon.com/wellarchitected/latest/reliability-pillar/welcome.html)
- [PostgreSQL High Availability](https://www.postgresql.org/docs/current/high-availability.html)
- [Redis Sentinel](https://redis.io/docs/management/sentinel/)
- [etcd Failure Modes](https://etcd.io/docs/v3.5/op-guide/failures/)
- [Google SRE Book: Managing Load](https://sre.google/sre-book/load-balancing-frontend/)

---

## Related ADRs

- [ADR-001: Use Firecracker for VM Isolation](./001-firecracker-vms.md)
- [ADR-002: Distributed Scheduler with Leader Election](./002-distributed-scheduler.md)
- [ADR-003: PostgreSQL + Redis for State Management](./003-state-management.md)

---

**Last Updated**: 2025-12-10
**Next Review**: 2026-06-10 (6 months)
**Owner**: Platform Architecture Team, SRE Team
