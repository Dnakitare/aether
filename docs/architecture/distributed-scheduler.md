# Distributed Scheduler Architecture

**Status:** Design Phase
**Target:** Phase 4 - Scalability
**Goal:** Scale from 1,000 to 10,000+ concurrent agents

---

## Current Architecture Limitations

### In-Memory Queue (`internal/scheduler/queue.go`)

**Structure:**
- Go container/heap for priority scheduling
- sync.RWMutex for synchronization
- O(log n) enqueue/dequeue, O(1) lookup by agent ID
- In-memory only - no persistence

**Limitations:**
1. **Single Instance**: Cannot horizontally scale
2. **No Persistence**: Queue lost on restart
3. **Memory Bound**: ~1,000 agents max before memory pressure
4. **No Fault Tolerance**: Single point of failure
5. **No Work Distribution**: Cannot share load across multiple schedulers

### Current Scheduler (`internal/scheduler/scheduler.go`)

**Structure:**
- Single goroutine scheduling loop (ticker-based)
- Read-write lock for node access
- Event channel for scheduling results
- In-memory node registry

**Limitations:**
1. **Sequential Processing**: One agent scheduled per interval
2. **No Parallelism**: Cannot leverage multiple CPU cores
3. **Tight Coupling**: Queue and placer tightly coupled to scheduler
4. **No Partitioning**: All agents compete for same lock

### Existing HA Support (`internal/ha/election.go`)

**Current Implementation:**
- etcd-based leader election ✅
- Leader/follower callbacks ✅
- Session TTL and keepalive ✅

**What's Missing:**
- Work distribution across non-leader nodes
- Active-active scheduler architecture
- Shard-based workload partitioning

---

## Distributed Scheduler Design

### Architecture Overview

```
┌─────────────────────────────────────────────────────────────────┐
│                         API Layer                               │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐            │
│  │ API Server 1│  │ API Server 2│  │ API Server 3│            │
│  └──────┬──────┘  └──────┬──────┘  └──────┬──────┘            │
│         │                │                │                     │
└─────────┼────────────────┼────────────────┼─────────────────────┘
          │                │                │
          ▼                ▼                ▼
    ┌─────────────────────────────────────────────┐
    │            Kafka Topic                      │
    │         "aether.scheduling.requests"        │
    │  ┌──────────┬──────────┬──────────┐        │
    │  │ Shard 0  │ Shard 1  │ Shard 2  │        │
    │  └──────────┴──────────┴──────────┘        │
    └─────────────────────────────────────────────┘
          │                │                │
          ▼                ▼                ▼
┌─────────────────────────────────────────────────────────────────┐
│                    Scheduler Layer                              │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐            │
│  │ Scheduler 1 │  │ Scheduler 2 │  │ Scheduler 3 │            │
│  │  (Shard 0)  │  │  (Shard 1)  │  │  (Shard 2)  │            │
│  └──────┬──────┘  └──────┬──────┘  └──────┬──────┘            │
│         │                │                │                     │
└─────────┼────────────────┼────────────────┼─────────────────────┘
          │                │                │
          └────────────────┼────────────────┘
                           ▼
              ┌────────────────────────┐
              │   Redis Cluster        │
              │ ┌──────────────────┐   │
              │ │  Node Registry   │   │
              │ │  Agent State     │   │
              │ │  Shard Mapping   │   │
              │ └──────────────────┘   │
              └────────────────────────┘
                           │
                           ▼
              ┌────────────────────────┐
              │   etcd Cluster         │
              │ ┌──────────────────┐   │
              │ │  Leader Election │   │
              │ │  Shard Assignment│   │
              │ │  Configuration   │   │
              │ └──────────────────┘   │
              └────────────────────────┘
```

---

## Component Design

### 1. Distributed Queue (Kafka)

#### Topic Configuration

**Topic:** `aether.scheduling.requests`

**Partitions:** 12 (configurable, default based on node count)
- More partitions = higher parallelism
- Recommendation: 2-4x number of scheduler instances
- Example: 3 schedulers → 12 partitions

**Replication Factor:** 3
- Fault tolerance for 2 broker failures
- Ensures no data loss on broker failure

**Retention:**
- Time: 24 hours (requests older than 24h are dropped)
- Size: 10GB per partition (prevents unbounded growth)
- Cleanup policy: delete (not compacted)

**Compression:** snappy
- Good CPU/compression ratio tradeoff
- ~2-3x compression for JSON payloads

#### Message Format

```json
{
  "schema_version": "v1",
  "request_id": "req_01HXYZ123456789",
  "agent_id": "agent_01HXYZ987654321",
  "tenant_id": "tenant_abc123",
  "priority": 10,
  "resources": {
    "cpu_cores": 2000,
    "memory_mb": 2048,
    "disk_mb": 10240
  },
  "constraints": {
    "node_selector": {
      "region": "us-west-2",
      "instance_type": "c5.xlarge"
    },
    "require_exclusive_node": false,
    "anti_affinity_tenant": true
  },
  "metadata": {
    "image": "python:3.11",
    "created_at": "2026-02-01T12:00:00Z",
    "retries": 0
  },
  "timeout_ms": 30000
}
```

#### Partitioning Strategy

**Key:** `tenant_id`

**Why tenant_id?**
- Even distribution across partitions (assuming uniform tenant distribution)
- Same-tenant agents go to same partition → locality for anti-affinity checks
- Enables per-tenant fairness (prevent one tenant from monopolizing resources)

**Hash Function:** murmur3 (Kafka default)

```go
partition := hash(tenant_id) % num_partitions
```

**Alternative:** Round-robin for multi-tenant fairness
- Use when single tenant has massive scale (e.g., 1 tenant with 5,000 agents)
- Custom partitioner: `(request_counter++) % num_partitions`

#### Consumer Configuration

**Consumer Group:** `aether-schedulers`
- All scheduler instances join same group
- Kafka automatically assigns partitions to consumers
- Rebalancing on scheduler join/leave

**Rebalancing Strategy:** Cooperative Sticky
- Minimizes partition movement on rebalance
- Reduces downtime during rolling deployments

**Offset Management:** Manual commit after placement
```go
// After successful placement
consumer.CommitMessages(ctx, message)

// On placement failure
if retryable {
    // Don't commit - message will be redelivered after visibility timeout
    return
}
// Commit to move past non-retryable errors
consumer.CommitMessages(ctx, message)
```

**Concurrency:** 10 goroutines per partition
- Each goroutine processes messages in parallel
- Rate limited to prevent overload

#### Failure Handling

**Retries:**
1. **Retriable Errors**: No suitable node, resource exhaustion, transient network errors
   - Don't commit offset
   - Message redelivered after `session.timeout.ms` (default 10s)
   - Increment retry counter in metadata
   - Max retries: 3

2. **Non-Retriable Errors**: Invalid request, validation failure, tenant not found
   - Commit offset immediately
   - Send error event to monitoring
   - Don't retry

**Dead Letter Queue (DLQ):**
- Topic: `aether.scheduling.dlq`
- Messages after max retries
- Manual inspection and reprocessing

**Exactly-Once Semantics:**
- Not required (idempotent placement operations)
- At-least-once delivery is sufficient
- Duplicate detection via request_id in Redis

---

### 2. Scheduler Sharding

#### Consistent Hashing

**Purpose:** Distribute node ownership across scheduler instances

**Hash Ring:**
```
Hash Space: 0 to 2^32-1 (uint32)

Virtual Nodes (vnodes): 100 per scheduler
- Improves distribution uniformity
- Reduces load variance on scheduler join/leave

Example with 3 schedulers:
┌─────────────────────────────────────────────────────┐
│ Hash Ring (0 to 4,294,967,295)                     │
│                                                      │
│  s1-v001 ─┐                                         │
│           │                                         │
│  s2-v001 ─┼─┐                                       │
│           │ │                                       │
│  s1-v002 ─┤ │                                       │
│           │ │                                       │
│  s3-v001 ─┤ ├─┐                                     │
│           │ │ │                                     │
│  s2-v002 ─┘ ├─┤                                     │
│             │ │                                     │
│  s1-v003 ───┘ ├─┐                                   │
│               │ │                                   │
│  [...]        └─┴─ [...]                            │
│                                                      │
└─────────────────────────────────────────────────────┘

Node Assignment:
node_hash = hash(node_id)
assigned_scheduler = find_next_on_ring(node_hash)
```

**Implementation:**

```go
type ConsistentHash struct {
    mu       sync.RWMutex
    ring     []uint32                    // Sorted hash values
    nodes    map[uint32]string           // Hash -> scheduler instance ID
    replicas int                         // Virtual nodes per scheduler
}

func (ch *ConsistentHash) Add(schedulerID string) {
    ch.mu.Lock()
    defer ch.mu.Unlock()

    for i := 0; i < ch.replicas; i++ {
        vnode := fmt.Sprintf("%s-v%03d", schedulerID, i)
        hash := murmur3(vnode)
        ch.ring = append(ch.ring, hash)
        ch.nodes[hash] = schedulerID
    }

    sort.Slice(ch.ring, func(i, j int) bool {
        return ch.ring[i] < ch.ring[j]
    })
}

func (ch *ConsistentHash) Get(nodeID string) string {
    ch.mu.RLock()
    defer ch.mu.RUnlock()

    if len(ch.ring) == 0 {
        return ""
    }

    hash := murmur3(nodeID)
    idx := sort.Search(len(ch.ring), func(i int) bool {
        return ch.ring[i] >= hash
    })

    // Wrap around if past end
    if idx == len(ch.ring) {
        idx = 0
    }

    return ch.nodes[ch.ring[idx]]
}

func (ch *ConsistentHash) Remove(schedulerID string) {
    ch.mu.Lock()
    defer ch.mu.Unlock()

    for i := 0; i < ch.replicas; i++ {
        vnode := fmt.Sprintf("%s-v%03d", schedulerID, i)
        hash := murmur3(vnode)
        delete(ch.nodes, hash)

        // Remove from ring
        idx := sort.Search(len(ch.ring), func(j int) bool {
            return ch.ring[j] >= hash
        })
        if idx < len(ch.ring) && ch.ring[idx] == hash {
            ch.ring = append(ch.ring[:idx], ch.ring[idx+1:]...)
        }
    }
}
```

#### Shard Assignment Storage (etcd)

**Key:** `/aether/scheduler/shards/{scheduler_id}`
**Value:** JSON with shard metadata

```json
{
  "scheduler_id": "scheduler-1",
  "instance_id": "pod-abc123",
  "hostname": "scheduler-1-abc123",
  "vnodes": [
    {"hash": 123456789, "vnode_id": "scheduler-1-v001"},
    {"hash": 234567890, "vnode_id": "scheduler-1-v002"},
    ...
  ],
  "assigned_nodes": [
    "node-001", "node-002", "node-003"
  ],
  "heartbeat": "2026-02-01T12:00:00Z",
  "status": "active"
}
```

**TTL:** 30 seconds with keepalive
- If scheduler crashes, shard expires automatically
- Triggers rebalancing

**Watch:** All schedulers watch `/aether/scheduler/shards/`
- On shard change: rebuild consistent hash ring
- On shard expiry: reassign nodes to other schedulers

---

### 3. State Synchronization (Redis)

#### Node Registry

**Key Pattern:** `aether:node:{node_id}`
**Value:** JSON node state
**TTL:** 60 seconds (refreshed by node heartbeat)

```json
{
  "id": "node-001",
  "name": "compute-node-us-west-2a-001",
  "labels": {
    "region": "us-west-2",
    "zone": "us-west-2a",
    "instance_type": "c5.4xlarge"
  },
  "capacity": {
    "cpu_cores": 16000,
    "memory_mb": 32768,
    "disk_mb": 512000
  },
  "allocated": {
    "cpu_cores": 8000,
    "memory_mb": 16384,
    "disk_mb": 102400
  },
  "agents": [
    "agent-001", "agent-002", "agent-003"
  ],
  "status": "ready",
  "last_heartbeat": "2026-02-01T12:00:00Z",
  "scheduler_owner": "scheduler-1"
}
```

**Indexing:**

```
# Set of all nodes
SADD aether:nodes node-001 node-002 node-003

# Index by scheduler owner
SADD aether:scheduler:scheduler-1:nodes node-001 node-002

# Index by status
SADD aether:nodes:ready node-001 node-002
SADD aether:nodes:draining node-003
```

**Operations:**

```go
// Get nodes owned by this scheduler
func (r *RedisNodeRegistry) GetOwnedNodes(ctx context.Context, schedulerID string) ([]*Node, error) {
    key := fmt.Sprintf("aether:scheduler:%s:nodes", schedulerID)
    nodeIDs, err := r.client.SMembers(ctx, key).Result()
    if err != nil {
        return nil, err
    }

    nodes := make([]*Node, 0, len(nodeIDs))
    for _, nodeID := range nodeIDs {
        node, err := r.GetNode(ctx, nodeID)
        if err != nil {
            continue // Skip nodes that disappeared
        }
        nodes = append(nodes, node)
    }

    return nodes, nil
}

// Atomic allocation check-and-set
func (r *RedisNodeRegistry) TryAllocate(ctx context.Context, nodeID string, agentID string, resources Resources) (bool, error) {
    // Lua script for atomic check-and-allocate
    script := `
        local node_key = KEYS[1]
        local node_json = redis.call('GET', node_key)
        if not node_json then
            return {0, 'node not found'}
        end

        local node = cjson.decode(node_json)
        local req_cpu = tonumber(ARGV[1])
        local req_mem = tonumber(ARGV[2])
        local req_disk = tonumber(ARGV[3])
        local agent_id = ARGV[4]

        -- Check available resources
        local avail_cpu = node.capacity.cpu_cores - node.allocated.cpu_cores
        local avail_mem = node.capacity.memory_mb - node.allocated.memory_mb
        local avail_disk = node.capacity.disk_mb - node.allocated.disk_mb

        if avail_cpu < req_cpu or avail_mem < req_mem or avail_disk < req_disk then
            return {0, 'insufficient resources'}
        end

        -- Allocate resources
        node.allocated.cpu_cores = node.allocated.cpu_cores + req_cpu
        node.allocated.memory_mb = node.allocated.memory_mb + req_mem
        node.allocated.disk_mb = node.allocated.disk_mb + req_disk
        table.insert(node.agents, agent_id)

        -- Save back
        redis.call('SET', node_key, cjson.encode(node))
        return {1, 'success'}
    `

    result, err := r.client.Eval(ctx, script,
        []string{fmt.Sprintf("aether:node:%s", nodeID)},
        resources.CPUCores, resources.MemoryMB, resources.DiskMB, agentID,
    ).Result()

    if err != nil {
        return false, err
    }

    res := result.([]interface{})
    success := res[0].(int64) == 1
    return success, nil
}
```

#### Agent Placement Cache

**Key Pattern:** `aether:placement:{agent_id}`
**Value:** JSON placement result
**TTL:** 5 minutes

```json
{
  "agent_id": "agent-001",
  "node_id": "node-001",
  "scheduler_id": "scheduler-1",
  "placed_at": "2026-02-01T12:00:00Z",
  "resources": {
    "cpu_cores": 2000,
    "memory_mb": 2048,
    "disk_mb": 10240
  }
}
```

**Purpose:**
- Deduplication: Prevent duplicate placement if request redelivered
- Observability: Quick lookup of placement decisions
- Debugging: Audit trail of scheduler decisions

#### Distributed Locks

Already implemented in Phase 2 (`internal/state/redis.go`)
- Used for critical sections (e.g., multi-node operations)
- Advisory locks with token-based ownership
- Watchdog for TTL extension

---

### 4. Scheduler Instance

#### Configuration

```go
type DistributedSchedulerConfig struct {
    // Instance identity
    InstanceID      string
    SchedulerID     string
    Hostname        string

    // Kafka configuration
    KafkaBrokers    []string
    KafkaTopic      string
    ConsumerGroup   string
    NumPartitions   int
    NumWorkers      int  // Goroutines per partition

    // Redis configuration
    RedisAddrs      []string
    RedisPassword   string

    // etcd configuration
    EtcdEndpoints   []string

    // Placement configuration
    PlacementStrategy  PlacementStrategy
    PlacementInterval  time.Duration

    // Consistent hashing
    VirtualNodes    int  // Default: 100
}
```

#### Startup Sequence

1. **Initialize Dependencies**
   - Connect to Kafka (consumer)
   - Connect to Redis (node registry, state)
   - Connect to etcd (leader election, shard assignment)

2. **Register Scheduler Instance**
   - Write to `/aether/scheduler/shards/{scheduler_id}`
   - Start heartbeat goroutine (15s interval)
   - Join consistent hash ring

3. **Subscribe to Kafka Topic**
   - Join consumer group
   - Wait for partition assignment
   - Start worker goroutines

4. **Fetch Owned Nodes**
   - Query Redis for nodes in shard
   - Build in-memory node cache
   - Start node watcher (refresh every 30s)

5. **Start Placement Loop**
   - Poll Kafka for scheduling requests
   - Place agents on owned nodes
   - Commit offsets on success

#### Placement Algorithm

```go
func (ds *DistributedScheduler) PlaceAgent(ctx context.Context, req *SchedulingRequest) error {
    // 1. Check for duplicate request
    if exists, err := ds.isDuplicate(ctx, req.RequestID); err != nil || exists {
        return err
    }

    // 2. Get nodes owned by this scheduler
    nodes, err := ds.nodeRegistry.GetOwnedNodes(ctx, ds.config.SchedulerID)
    if err != nil {
        return fmt.Errorf("failed to get owned nodes: %w", err)
    }

    // 3. Filter candidates using placement constraints
    candidates := ds.placer.FilterCandidates(req, nodes)
    if len(candidates) == 0 {
        return fmt.Errorf("no suitable nodes found (owned: %d, after filter: 0)", len(nodes))
    }

    // 4. Select best node using placement strategy
    node, err := ds.placer.SelectNode(req, candidates)
    if err != nil {
        return fmt.Errorf("placement failed: %w", err)
    }

    // 5. Atomic allocation (check-and-set in Redis)
    success, err := ds.nodeRegistry.TryAllocate(ctx, node.ID, req.AgentID, req.Resources)
    if err != nil {
        return fmt.Errorf("allocation failed: %w", err)
    }
    if !success {
        // Race condition - node capacity changed, retry
        return ErrRetryable
    }

    // 6. Cache placement result
    if err := ds.cachePlacement(ctx, req.AgentID, node.ID); err != nil {
        ds.logger.Warn("failed to cache placement", "error", err)
        // Non-fatal, continue
    }

    // 7. Send placement event
    ds.sendEvent(ctx, ScheduleEvent{
        AgentID:   req.AgentID,
        NodeID:    node.ID,
        Success:   true,
        Timestamp: time.Now(),
    })

    ds.logger.Info("agent placed",
        "agent_id", req.AgentID,
        "node_id", node.ID,
        "scheduler_id", ds.config.SchedulerID,
    )

    return nil
}
```

#### Failure Scenarios

**Scenario 1: Scheduler Crashes**

```
Time 0: Scheduler-1 crashes (owns nodes: node-001, node-002, node-003)
Time 15s: etcd shard entry expires (TTL timeout)
Time 15s: Other schedulers detect shard expiry (via watch)
Time 16s: Consistent hash ring rebuilt (removes scheduler-1 vnodes)
Time 17s: Node-001, node-002, node-003 reassigned to scheduler-2, scheduler-3
Time 18s: New scheduling requests for these nodes go to new owners
Time 20s: Scheduler-1 restarts, rejoins ring, claims new nodes
```

**Recovery:** Automatic, handled by consistent hashing and etcd TTL

**Scenario 2: Kafka Partition Rebalance**

```
Time 0: Scheduler-4 joins cluster
Time 1s: Kafka triggers rebalance (cooperative sticky strategy)
Time 2s: Partitions redistributed (minimal movement)
Time 3s: Schedulers resume processing (some in-flight requests may be duplicated)
Time 3s: Duplicate detection via request_id prevents double placement
```

**Recovery:** Kafka consumer group handles automatically, idempotency prevents issues

**Scenario 3: Redis Unavailable**

```
Time 0: Redis primary fails
Time 1s: Redis Sentinel detects failure
Time 2s: Sentinel promotes replica to primary
Time 3s: Redis clients reconnect to new primary
Time 4s: Schedulers resume operations
```

**During Outage:**
- Placement requests fail (cannot read node state)
- Requests remain in Kafka (not committed)
- Automatically retried after Redis recovers

**Scenario 4: Split Brain (etcd Partition)**

```
Time 0: Network partition splits etcd cluster
Time 1s: Minority partition loses quorum (cannot write)
Time 2s: Schedulers in minority partition cannot update heartbeat
Time 32s: Shard entries in minority partition expire (30s TTL + 2s)
Time 33s: Majority partition reassigns nodes to active schedulers
Time 60s: Network partition heals
Time 61s: Schedulers in former minority rejoin, reclaim nodes
```

**Protection:** etcd quorum requirement prevents split-brain writes

---

## Migration Strategy

### Phase 1: Dual-Mode Operation (Week 1)

**Goal:** Run old and new scheduler side-by-side

**Implementation:**
1. Deploy distributed scheduler (read-only mode)
   - Consumes from Kafka
   - Places agents
   - Does NOT write to production state

2. Old scheduler remains active
   - Primary scheduler
   - Handles all production traffic

3. Compare results
   - Log placement decisions from both
   - Alert on divergence
   - Validate correctness

**Verification:**
- 0% divergence in placement decisions
- New scheduler handles 100% of test traffic

### Phase 2: Canary Deployment (Week 2)

**Goal:** Route 10% of traffic to new scheduler

**Implementation:**
1. Feature flag: `distributed_scheduler_enabled`
   - 10% of API servers enable flag
   - Requests from these servers go to Kafka
   - Remaining 90% go to old scheduler

2. Monitor metrics:
   - Placement latency (p50, p99)
   - Error rate
   - Node utilization
   - Agent startup time

**Rollback Plan:**
- Disable feature flag (instant rollback)
- Old scheduler still running, no downtime

**Success Criteria:**
- p99 latency < 100ms (same as old scheduler)
- Error rate < 0.1%
- No resource allocation conflicts

### Phase 3: Gradual Rollout (Week 3)

**Increments:**
- Day 1: 10% → 25%
- Day 2: 25% → 50%
- Day 3: 50% → 75%
- Day 4: 75% → 100%

**At Each Increment:**
- Monitor for 4 hours
- Check metrics (latency, errors, utilization)
- Verify no regressions
- Rollback if issues detected

### Phase 4: Deprecate Old Scheduler (Week 4)

**Steps:**
1. 100% traffic on new scheduler (old scheduler idle)
2. Monitor for 1 week
3. Remove old scheduler code
4. Update documentation

---

## Performance Targets

### Throughput

| Metric | Current (In-Memory) | Target (Distributed) |
|--------|---------------------|----------------------|
| Max agents | 1,000 | 10,000+ |
| Placements/sec (single scheduler) | 10 | 100 |
| Placements/sec (cluster) | 10 | 1,000+ |

### Latency

| Metric | Target |
|--------|--------|
| p50 placement latency | < 10ms |
| p95 placement latency | < 50ms |
| p99 placement latency | < 100ms |

### Scalability

| Metric | Target |
|--------|--------|
| Schedulers per cluster | 3-10 |
| Nodes per scheduler | 100-1,000 |
| Kafka partitions | 12 (3 schedulers × 4) |
| Redis memory per 10K agents | < 500MB |

---

## Operational Considerations

### Monitoring

**Scheduler Metrics:**
- `scheduler_placements_total{scheduler_id,status}` - Counter of placement attempts
- `scheduler_placement_duration_seconds` - Histogram of placement latency
- `scheduler_owned_nodes{scheduler_id}` - Gauge of nodes owned by scheduler
- `scheduler_kafka_lag{partition}` - Gauge of consumer lag
- `scheduler_placement_errors_total{scheduler_id,error_type}` - Counter of errors

**Node Metrics:**
- `node_capacity_cpu_cores{node_id}` - Gauge of total CPU
- `node_allocated_cpu_cores{node_id}` - Gauge of allocated CPU
- `node_utilization_percent{node_id}` - Gauge of utilization
- `node_agent_count{node_id}` - Gauge of agents on node

**Kafka Metrics:**
- `kafka_consumer_lag{partition}` - Lag per partition
- `kafka_consumer_offset{partition}` - Current offset
- `kafka_messages_consumed_total{partition}` - Counter of messages

**Alerts:**
- Scheduler down (heartbeat missing for 1 minute)
- High placement latency (p99 > 200ms)
- Kafka lag > 1000 messages
- Placement error rate > 1%
- Node unavailable (heartbeat missing for 2 minutes)

### Debugging

**Placement Decision Trail:**
1. Request received: `request_id`, `agent_id`, `tenant_id`
2. Kafka partition: Which partition consumed from
3. Scheduler instance: Which scheduler handled request
4. Node candidates: Nodes considered (with filter reasons)
5. Selected node: Final placement decision
6. Allocation result: Success or failure (with reason)

**Log Example:**
```
{
  "timestamp": "2026-02-01T12:00:00.123Z",
  "level": "INFO",
  "component": "distributed_scheduler",
  "scheduler_id": "scheduler-1",
  "request_id": "req_01HXYZ123",
  "agent_id": "agent_01HXYZ987",
  "tenant_id": "tenant_abc",
  "owned_nodes": 45,
  "candidates": [
    {"node_id": "node-001", "utilization": 45.2, "score": 0.85},
    {"node_id": "node-012", "utilization": 38.7, "score": 0.92},
    {"node_id": "node-023", "utilization": 52.1, "score": 0.78}
  ],
  "selected_node": "node-012",
  "placement_duration_ms": 12.5,
  "message": "agent placed successfully"
}
```

### Capacity Planning

**Scheduler Sizing:**
- CPU: 2-4 cores per scheduler instance
- Memory: 2-4GB per scheduler instance (in-memory node cache)
- Network: 100 Mbps (Kafka traffic)

**Redis Sizing:**
- Memory: ~50KB per node, ~5KB per agent
- For 10,000 agents on 1,000 nodes: ~100MB
- Recommendation: 1GB Redis memory (10x headroom)

**Kafka Sizing:**
- Disk: 10GB per partition (1 week retention)
- For 12 partitions: 120GB
- Throughput: 10MB/s write, 30MB/s read (3 consumers)

---

## Open Questions

1. **Node reassignment latency**: When a scheduler crashes, how long until nodes are reassigned?
   - **Answer**: ~17 seconds (15s etcd TTL + 2s rebuild)
   - **Acceptable?** Yes for initial implementation, optimize later if needed

2. **Cross-shard placement**: What if best node is owned by different scheduler?
   - **Answer**: Not supported in v1 (scheduler only places on owned nodes)
   - **Future**: Cross-shard placement with coordination protocol

3. **Scheduler scaling**: How to add/remove schedulers without downtime?
   - **Answer**: Kafka consumer group rebalancing (automatic)
   - **Concern**: Brief placement latency spike during rebalance

4. **Fairness**: How to prevent one tenant from monopolizing resources?
   - **Answer**: Kafka partitioning by tenant_id provides isolation
   - **Future**: Per-tenant quotas and rate limiting

---

## Next Steps

1. **Implement Core Components** (Week 5-6)
   - Kafka producer/consumer
   - Consistent hashing
   - Redis node registry
   - Distributed scheduler loop

2. **Integration Testing** (Week 7)
   - Multi-scheduler setup
   - Failure injection tests
   - Load testing (1,000 → 10,000 agents)

3. **Migration** (Week 8)
   - Dual-mode operation
   - Canary deployment
   - Full rollout

---

## References

- Current Scheduler: `internal/scheduler/scheduler.go`
- Current Queue: `internal/scheduler/queue.go`
- Placement Logic: `internal/scheduler/placement.go`
- Leader Election: `internal/ha/election.go`
- Remediation Plan: `/Users/overlord/.claude/plans/zippy-whistling-sketch.md`
