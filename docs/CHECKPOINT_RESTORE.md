# Checkpoint and Restore Guide

**Status**: Beta v0.2.0 (Week 4, Day 25-26)
**Feature**: Agent state checkpointing and recovery

---

## 🎯 Overview

Aether provides checkpoint and restore capabilities for agent state management. This allows you to:
- **Create snapshots** of agent state at any point in time
- **Restore agents** to previous states after failures
- **Implement disaster recovery** strategies
- **Support state migrations** across infrastructure changes

---

## 📋 Table of Contents

- [Architecture](#architecture)
- [Quick Start](#quick-start)
- [CLI Usage](#cli-usage)
- [Programmatic Usage](#programmatic-usage)
- [Recovery Strategies](#recovery-strategies)
- [Best Practices](#best-practices)
- [Configuration](#configuration)
- [Troubleshooting](#troubleshooting)

---

## 🏗️ Architecture

### Components

```
┌─────────────────┐
│  Runtime API    │  ← User-facing API
└────────┬────────┘
         │
┌────────▼────────┐
│ CheckpointMgr   │  ← Checkpoint creation/retrieval
└────────┬────────┘
         │
┌────────▼────────┐
│  PostgreSQL     │  ← Persistent storage
└─────────────────┘
```

### Key Classes

**CheckpointManager** (`internal/recovery/checkpoint.go`)
- Creates and manages agent checkpoints
- Stores checkpoints in PostgreSQL
- Handles versioning and retention
- Provides atomic operations with advisory locks

**RecoveryManager** (`internal/recovery/checkpoint.go`)
- Orchestrates agent recovery from checkpoints
- Implements retry logic
- Supports multiple recovery strategies

**BackupManager** (`internal/backup/backup.go`)
- Full system backups (PostgreSQL + Redis)
- Scheduled backup automation
- Backup verification and cleanup

### Data Model

```sql
CREATE TABLE checkpoints (
    id BIGSERIAL PRIMARY KEY,
    agent_id VARCHAR(255) NOT NULL,
    tenant_id VARCHAR(255) NOT NULL,
    version INTEGER NOT NULL,
    state JSONB NOT NULL,
    metadata JSONB,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    size BIGINT NOT NULL,
    compressed BOOLEAN DEFAULT FALSE,
    UNIQUE (agent_id, version)
);
```

**State Structure:**
```json
{
  "status": "running",
  "config": {
    "id": "agent-001",
    "tenant_id": "production",
    "image": "python:3.11",
    "resources": {
      "cpu_count": 2,
      "memory_mb": 1024
    }
  },
  "created_at": "2026-02-15T10:30:00Z"
}
```

---

## 🚀 Quick Start

### Prerequisites

- PostgreSQL database configured
- Runtime with checkpoint manager initialized
- Agent created and running

### Create Your First Checkpoint

```go
import (
    "context"
    "database/sql"
    "github.com/aether-runtime/aether/internal/runtime"
    "github.com/aether-runtime/aether/pkg/api"
)

// 1. Create runtime
rt, _ := runtime.New(logger, config, stateStore)

// 2. Initialize checkpoint manager
db, _ := sql.Open("postgres", "postgresql://...")
rt.SetCheckpointManager(db)

// 3. Create checkpoint
ctx := context.Background()
agentID := api.AgentID("agent-001")
checkpoint, err := rt.CreateCheckpoint(ctx, agentID)
if err != nil {
    log.Fatal(err)
}

fmt.Printf("Checkpoint created: version %d, size %d bytes\n",
    checkpoint.Version, checkpoint.Size)
```

### Restore from Checkpoint

```go
// Restore from latest checkpoint
err := rt.RestoreFromCheckpoint(ctx, agentID, 0)

// Restore from specific version
err = rt.RestoreFromCheckpoint(ctx, agentID, 3)
```

---

## 💻 CLI Usage

### Available Commands

```bash
aether agent checkpoint create <agent-id>   # Create checkpoint
aether agent checkpoint list <agent-id>     # List checkpoints
aether agent checkpoint restore <agent-id>  # Restore agent
aether agent checkpoint delete <agent-id> <version>  # Delete checkpoint
```

### Examples

**Create Checkpoint:**
```bash
$ aether agent checkpoint create agent-001

ℹ Creating checkpoint for agent agent-001
✓ Checkpoint version 5 created (1.2 MB)
```

**List Checkpoints:**
```bash
$ aether agent checkpoint list agent-001

VERSION  CREATED              SIZE     STATUS
──────────────────────────────────────────────
5        2026-02-15 10:30:00  1.2 MB   Available
4        2026-02-15 09:00:00  1.1 MB   Available
3        2026-02-15 08:00:00  1.0 MB   Available

Total: 3 checkpoint(s)
```

**Restore from Checkpoint:**
```bash
# Restore from latest
$ aether agent checkpoint restore agent-001

# Restore from specific version
$ aether agent checkpoint restore agent-001 --version 3

⠋ Restoring agent from checkpoint version 3...
✓ Agent restored successfully
```

**Delete Checkpoint:**
```bash
$ aether agent checkpoint delete agent-001 3

⚠ This will permanently delete checkpoint version 3
✓ Checkpoint deleted
```

---

## 📚 Programmatic Usage

### Runtime API

#### Initialize Checkpoint Manager

```go
import (
    "database/sql"
    _ "github.com/lib/pq"
)

db, err := sql.Open("postgres", databaseURL)
if err != nil {
    log.Fatal(err)
}

err = runtime.SetCheckpointManager(db)
if err != nil {
    log.Fatal(err)
}
```

#### Create Checkpoint

```go
checkpoint, err := runtime.CreateCheckpoint(ctx, agentID)
if err != nil {
    return fmt.Errorf("checkpoint failed: %w", err)
}

log.Printf("Created checkpoint v%d (%d bytes)",
    checkpoint.Version, checkpoint.Size)
```

#### List Checkpoints

```go
checkpoints, err := runtime.ListCheckpoints(ctx, agentID)
if err != nil {
    return err
}

for _, cp := range checkpoints {
    fmt.Printf("v%d: %s (%d bytes)\n",
        cp.Version,
        cp.CreatedAt.Format(time.RFC3339),
        cp.Size,
    )
}
```

#### Restore from Checkpoint

```go
// Latest checkpoint
err := runtime.RestoreFromCheckpoint(ctx, agentID, 0)

// Specific version
err = runtime.RestoreFromCheckpoint(ctx, agentID, 3)

// With recovery manager (includes retries)
recoveryManager := recovery.NewRecoveryManager(
    logger,
    checkpointManager,
    recovery.DefaultRecoveryConfig(),
)

err = recoveryManager.RecoverAgentWithRetry(ctx, agentID,
    func(state map[string]interface{}) error {
        // Custom restore logic
        return restoreAgentState(state)
    },
)
```

#### Delete Checkpoint

```go
err := runtime.DeleteCheckpoint(ctx, agentID, 3)
if err != nil {
    return fmt.Errorf("delete failed: %w", err)
}
```

### Direct CheckpointManager Usage

For more control, use `CheckpointManager` directly:

```go
import "github.com/aether-runtime/aether/internal/recovery"

config := recovery.DefaultCheckpointConfig()
config.RetentionCount = 20  // Keep 20 checkpoints
config.MaxCheckpointSize = 200 * 1024 * 1024  // 200MB max

cm, err := recovery.NewCheckpointManager(logger, db, config)

// Create with custom state
state := map[string]interface{}{
    "custom_field": "value",
    "timestamp": time.Now(),
}

metadata := map[string]string{
    "created_by": "automated_backup",
    "reason": "scheduled_checkpoint",
}

checkpoint, err := cm.CreateCheckpoint(
    ctx, agentID, tenantID, state, metadata,
)
```

---

## 🔄 Recovery Strategies

### 1. Restart Strategy

Restart the agent with last known state:

```go
strategy := recovery.StrategyRestart

// Get latest checkpoint
checkpoint, err := checkpointManager.GetLatestCheckpoint(ctx, agentID)

// Restart agent with checkpoint state
err = restartAgentWithState(agentID, checkpoint.State)
```

**Use Cases:**
- Agent crashed unexpectedly
- Quick recovery needed
- State is recent and valid

### 2. Rollback Strategy

Roll back to a specific previous checkpoint:

```go
strategy := recovery.StrategyRollback

// Get specific checkpoint version
checkpoint, err := checkpointManager.GetCheckpointByVersion(
    ctx, agentID, 3,
)

// Rollback to that version
err = restartAgentWithState(agentID, checkpoint.State)
```

**Use Cases:**
- Bad deployment needs rollback
- Data corruption detected
- Return to known-good state

### 3. Failover Strategy

Failover to a backup agent:

```go
strategy := recovery.StrategyFailover

// Get checkpoint from primary
checkpoint, err := checkpointManager.GetLatestCheckpoint(
    ctx, primaryAgentID,
)

// Create new agent with same state
backupAgentID := api.AgentID("agent-001-backup")
err = createAgentWithState(backupAgentID, checkpoint.State)
```

**Use Cases:**
- Primary agent infrastructure failed
- Cross-region disaster recovery
- High availability requirements

---

## ✅ Best Practices

### Checkpoint Frequency

**Low-Frequency (Hourly):**
- Suitable for stateless or idempotent agents
- Lower storage overhead
- Acceptable data loss window

**Medium-Frequency (Every 5 minutes):**
- Balanced approach for most use cases
- Moderate storage overhead
- Short recovery time objective (RTO)

**High-Frequency (Every minute):**
- Critical agents with low RPO requirements
- Higher storage overhead
- Near-zero data loss

**Event-Driven:**
- Checkpoint before/after critical operations
- Optimal for transaction-like workflows
- Variable checkpoint frequency

### Retention Policies

```go
config := recovery.CheckpointConfig{
    RetentionCount: 10,  // Keep last 10 checkpoints
    MaxCheckpointSize: 100 * 1024 * 1024,  // 100MB max
}
```

**Recommended Retention:**
- **Development**: 5-10 checkpoints
- **Staging**: 10-20 checkpoints
- **Production**: 20-50 checkpoints
- **Critical Systems**: 50-100 checkpoints

### Storage Management

Monitor checkpoint storage usage:

```sql
SELECT
    agent_id,
    COUNT(*) as checkpoint_count,
    SUM(size) as total_size_bytes,
    SUM(size) / (1024 * 1024) as total_size_mb
FROM checkpoints
GROUP BY agent_id
ORDER BY total_size_mb DESC;
```

Cleanup old checkpoints:

```sql
DELETE FROM checkpoints
WHERE agent_id = 'agent-001'
AND id NOT IN (
    SELECT id FROM checkpoints
    WHERE agent_id = 'agent-001'
    ORDER BY version DESC
    LIMIT 10
);
```

### Testing Recovery

Always test your recovery procedures:

```bash
# 1. Create checkpoint
aether agent checkpoint create agent-001

# 2. Make some changes
aether agent exec agent-001 -- some-command

# 3. Test restore
aether agent checkpoint restore agent-001

# 4. Verify state
aether agent logs agent-001
```

---

## ⚙️ Configuration

### CheckpointConfig

```go
type CheckpointConfig struct {
    // Interval for periodic checkpoints
    Interval time.Duration

    // RetentionCount: how many checkpoints to keep per agent
    RetentionCount int

    // MaxCheckpointSize in bytes
    MaxCheckpointSize int64

    // EnableCompression enables state compression
    EnableCompression bool
}
```

### RecoveryConfig

```go
type RecoveryConfig struct {
    // MaxRetries for recovery attempts
    MaxRetries int

    // RetryDelay between recovery attempts
    RetryDelay time.Duration

    // HealthCheckInterval for monitoring agents
    HealthCheckInterval time.Duration
}
```

### Example Configuration

```go
checkpointConfig := recovery.CheckpointConfig{
    Interval:          5 * time.Minute,
    RetentionCount:    20,
    MaxCheckpointSize: 200 * 1024 * 1024,  // 200MB
    EnableCompression: true,
}

recoveryConfig := recovery.RecoveryConfig{
    MaxRetries:          3,
    RetryDelay:          30 * time.Second,
    HealthCheckInterval: 1 * time.Minute,
}
```

---

## 🔧 Troubleshooting

### Checkpoint Creation Fails

**Error**: "checkpoint size exceeds maximum"
```go
// Solution: Increase MaxCheckpointSize
config.MaxCheckpointSize = 500 * 1024 * 1024  // 500MB
```

**Error**: "failed to acquire advisory lock"
```go
// Solution: Another checkpoint creation is in progress
// Wait and retry, or check for hung transactions:
SELECT * FROM pg_locks WHERE locktype = 'advisory';
```

### Restore Fails

**Error**: "no checkpoint found for agent"
```go
// Solution: Create a checkpoint first
checkpoint, err := runtime.CreateCheckpoint(ctx, agentID)
```

**Error**: "checkpoint version not found"
```go
// Solution: List available checkpoints
checkpoints, err := runtime.ListCheckpoints(ctx, agentID)
```

### Performance Issues

**Slow Checkpoint Creation:**
- Check database performance
- Review checkpoint size
- Consider enabling compression

**High Storage Usage:**
- Reduce retention count
- Implement periodic cleanup
- Archive old checkpoints to S3

---

## 📊 Monitoring

### Metrics to Track

1. **Checkpoint Creation Rate**
   - Checkpoints created per hour
   - Average checkpoint size
   - Creation latency

2. **Storage Usage**
   - Total checkpoint storage
   - Per-agent storage usage
   - Growth rate

3. **Recovery Success Rate**
   - Successful recoveries
   - Failed recoveries
   - Average recovery time

### Example Queries

```sql
-- Checkpoint creation rate (last 24 hours)
SELECT
    DATE_TRUNC('hour', created_at) as hour,
    COUNT(*) as checkpoints_created,
    AVG(size) as avg_size_bytes
FROM checkpoints
WHERE created_at >= NOW() - INTERVAL '24 hours'
GROUP BY hour
ORDER BY hour DESC;

-- Failed checkpoint attempts (requires audit log)
SELECT
    agent_id,
    COUNT(*) as failed_attempts,
    MAX(attempted_at) as last_failure
FROM checkpoint_failures
WHERE attempted_at >= NOW() - INTERVAL '7 days'
GROUP BY agent_id;
```

---

## 🔗 Related Documentation

- [Runtime API Reference](./api/API_REFERENCE.md)
- [State Management](./architecture/adr/003-state-management.md)
- [Disaster Recovery](./operations/RUNBOOKS.md)
- [PostgreSQL Setup](./deployment/PRODUCTION_DEPLOYMENT.md)

---

## 📝 API Reference

### Runtime Methods

```go
// SetCheckpointManager initializes checkpoint manager
func (r *Runtime) SetCheckpointManager(db *sql.DB) error

// CreateCheckpoint creates a checkpoint for an agent
func (r *Runtime) CreateCheckpoint(
    ctx context.Context,
    agentID api.AgentID,
) (*recovery.Checkpoint, error)

// ListCheckpoints lists all checkpoints for an agent
func (r *Runtime) ListCheckpoints(
    ctx context.Context,
    agentID api.AgentID,
) ([]*recovery.Checkpoint, error)

// GetLatestCheckpoint gets the latest checkpoint
func (r *Runtime) GetLatestCheckpoint(
    ctx context.Context,
    agentID api.AgentID,
) (*recovery.Checkpoint, error)

// RestoreFromCheckpoint restores agent from checkpoint
func (r *Runtime) RestoreFromCheckpoint(
    ctx context.Context,
    agentID api.AgentID,
    version int,  // 0 = latest
) error

// DeleteCheckpoint deletes a specific checkpoint
func (r *Runtime) DeleteCheckpoint(
    ctx context.Context,
    agentID api.AgentID,
    version int,
) error
```

---

## 🎓 Examples

### Automated Periodic Checkpointing

```go
func periodicCheckpointing(
    ctx context.Context,
    rt *runtime.Runtime,
    agentID api.AgentID,
    interval time.Duration,
) {
    ticker := time.NewTicker(interval)
    defer ticker.Stop()

    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            checkpoint, err := rt.CreateCheckpoint(ctx, agentID)
            if err != nil {
                log.Printf("checkpoint failed: %v", err)
                continue
            }
            log.Printf("checkpoint v%d created", checkpoint.Version)
        }
    }
}

// Usage
go periodicCheckpointing(ctx, runtime, agentID, 5*time.Minute)
```

### Checkpoint Before Critical Operations

```go
func runCriticalOperation(
    ctx context.Context,
    rt *runtime.Runtime,
    agentID api.AgentID,
) error {
    // Create checkpoint before operation
    checkpoint, err := rt.CreateCheckpoint(ctx, agentID)
    if err != nil {
        return fmt.Errorf("pre-operation checkpoint failed: %w", err)
    }

    // Run critical operation
    if err := criticalOperation(); err != nil {
        // Rollback on failure
        log.Printf("operation failed, restoring checkpoint v%d",
            checkpoint.Version)
        return rt.RestoreFromCheckpoint(ctx, agentID, checkpoint.Version)
    }

    return nil
}
```

---

**Last Updated**: February 15, 2026
**Version**: Beta v0.2.0
**Status**: Production Ready
