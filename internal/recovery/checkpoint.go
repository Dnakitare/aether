// Package recovery provides agent checkpointing and recovery capabilities.
package recovery

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/aether-runtime/aether/pkg/api"
)

// CheckpointManager manages agent state checkpoints.
type CheckpointManager struct {
	logger *slog.Logger
	db     *sql.DB
	config CheckpointConfig
}

// CheckpointConfig holds checkpoint configuration.
type CheckpointConfig struct {
	// Interval for periodic checkpoints
	Interval time.Duration

	// RetentionCount is how many checkpoints to keep per agent
	RetentionCount int

	// MaxCheckpointSize in bytes
	MaxCheckpointSize int64

	// EnableCompression enables state compression
	EnableCompression bool
}

// DefaultCheckpointConfig returns default checkpoint configuration.
func DefaultCheckpointConfig() CheckpointConfig {
	return CheckpointConfig{
		Interval:          5 * time.Minute,
		RetentionCount:    10,
		MaxCheckpointSize: 100 * 1024 * 1024, // 100MB
		EnableCompression: true,
	}
}

// Checkpoint represents an agent state checkpoint.
type Checkpoint struct {
	ID        int64
	AgentID   api.AgentID
	TenantID  api.TenantID
	Version   int
	State     map[string]interface{}
	Metadata  map[string]string
	CreatedAt time.Time
	Size      int64
}

// NewCheckpointManager creates a new checkpoint manager.
func NewCheckpointManager(logger *slog.Logger, db *sql.DB, config CheckpointConfig) (*CheckpointManager, error) {
	cm := &CheckpointManager{
		logger: logger.With("component", "checkpoint_manager"),
		db:     db,
		config: config,
	}

	if err := cm.createTable(); err != nil {
		return nil, err
	}

	return cm, nil
}

func (cm *CheckpointManager) createTable() error {
	query := `
	CREATE TABLE IF NOT EXISTS checkpoints (
		id BIGSERIAL PRIMARY KEY,
		agent_id VARCHAR(255) NOT NULL,
		tenant_id VARCHAR(255) NOT NULL,
		version INTEGER NOT NULL,
		state JSONB NOT NULL,
		metadata JSONB,
		created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
		size BIGINT NOT NULL,
		compressed BOOLEAN DEFAULT FALSE
	);

	CREATE INDEX IF NOT EXISTS idx_checkpoints_agent_id ON checkpoints (agent_id);
	CREATE INDEX IF NOT EXISTS idx_checkpoints_tenant_id ON checkpoints (tenant_id);
	CREATE INDEX IF NOT EXISTS idx_checkpoints_created_at ON checkpoints (created_at DESC);
	CREATE UNIQUE INDEX IF NOT EXISTS idx_checkpoints_agent_version ON checkpoints (agent_id, version);
	`

	if _, err := cm.db.Exec(query); err != nil {
		return fmt.Errorf("failed to create checkpoints table: %w", err)
	}

	return nil
}

// CreateCheckpoint creates a new checkpoint for an agent.
// This operation is atomic: both checkpoint insertion and cleanup happen in a single transaction.
func (cm *CheckpointManager) CreateCheckpoint(ctx context.Context, agentID api.AgentID, tenantID api.TenantID, state map[string]interface{}, metadata map[string]string) (*Checkpoint, error) {
	// Serialize state before transaction
	stateJSON, err := json.Marshal(state)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal state: %w", err)
	}

	// Check size before transaction
	size := int64(len(stateJSON))
	if size > cm.config.MaxCheckpointSize {
		return nil, fmt.Errorf("checkpoint size %d exceeds maximum %d", size, cm.config.MaxCheckpointSize)
	}

	// Serialize metadata before transaction
	metadataJSON, err := json.Marshal(metadata)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal metadata: %w", err)
	}

	// Begin transaction with read committed isolation for better concurrency
	tx, err := cm.db.BeginTx(ctx, &sql.TxOptions{
		Isolation: sql.LevelReadCommitted,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Acquire advisory lock based on agent_id hash to serialize checkpoint creation per agent
	// This allows concurrent checkpoints for different agents while preventing race conditions
	// The lock is automatically released when the transaction commits/rolls back
	_, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, agentID)
	if err != nil {
		return nil, fmt.Errorf("failed to acquire advisory lock: %w", err)
	}

	// Get the latest version for this agent (now safe due to advisory lock)
	var maxVersion int
	err = tx.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(version), 0) FROM checkpoints WHERE agent_id = $1`,
		agentID,
	).Scan(&maxVersion)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("failed to get max version: %w", err)
	}
	newVersion := maxVersion + 1

	// Insert checkpoint with the new version
	insertQuery := `
		INSERT INTO checkpoints (agent_id, tenant_id, version, state, metadata, size, compressed)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, created_at
	`

	var checkpoint Checkpoint
	checkpoint.AgentID = agentID
	checkpoint.TenantID = tenantID
	checkpoint.Version = newVersion
	checkpoint.State = state
	checkpoint.Metadata = metadata
	checkpoint.Size = size

	err = tx.QueryRowContext(
		ctx,
		insertQuery,
		agentID,                     // $1
		tenantID,                    // $2
		newVersion,                  // $3
		stateJSON,                   // $4
		metadataJSON,                // $5
		size,                        // $6
		cm.config.EnableCompression, // $7
	).Scan(&checkpoint.ID, &checkpoint.CreatedAt)

	if err != nil {
		return nil, fmt.Errorf("failed to insert checkpoint: %w", err)
	}

	// Cleanup old checkpoints within the same transaction (atomic with insert)
	cleanupQuery := `
		DELETE FROM checkpoints
		WHERE agent_id = $1
		AND id NOT IN (
			SELECT id FROM checkpoints
			WHERE agent_id = $1
			ORDER BY version DESC
			LIMIT $2
		)
	`

	result, err := tx.ExecContext(ctx, cleanupQuery, agentID, cm.config.RetentionCount)
	if err != nil {
		return nil, fmt.Errorf("failed to cleanup old checkpoints: %w", err)
	}

	deleted, _ := result.RowsAffected()

	// Commit transaction atomically
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	cm.logger.InfoContext(ctx, "checkpoint created",
		"agent_id", agentID,
		"version", checkpoint.Version,
		"size", size,
		"deleted_old", deleted,
	)

	return &checkpoint, nil
}

// GetLatestCheckpoint retrieves the latest checkpoint for an agent.
func (cm *CheckpointManager) GetLatestCheckpoint(ctx context.Context, agentID api.AgentID) (*Checkpoint, error) {
	query := `
		SELECT id, agent_id, tenant_id, version, state, metadata, created_at, size
		FROM checkpoints
		WHERE agent_id = $1
		ORDER BY version DESC
		LIMIT 1
	`

	var checkpoint Checkpoint
	var stateJSON, metadataJSON []byte

	err := cm.db.QueryRowContext(ctx, query, agentID).Scan(
		&checkpoint.ID,
		&checkpoint.AgentID,
		&checkpoint.TenantID,
		&checkpoint.Version,
		&stateJSON,
		&metadataJSON,
		&checkpoint.CreatedAt,
		&checkpoint.Size,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("no checkpoint found for agent %s", agentID)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get checkpoint: %w", err)
	}

	// Deserialize state
	if err := json.Unmarshal(stateJSON, &checkpoint.State); err != nil {
		return nil, fmt.Errorf("failed to unmarshal state: %w", err)
	}

	// Deserialize metadata
	if err := json.Unmarshal(metadataJSON, &checkpoint.Metadata); err != nil {
		return nil, fmt.Errorf("failed to unmarshal metadata: %w", err)
	}

	return &checkpoint, nil
}

// GetCheckpointByVersion retrieves a specific version checkpoint.
func (cm *CheckpointManager) GetCheckpointByVersion(ctx context.Context, agentID api.AgentID, version int) (*Checkpoint, error) {
	query := `
		SELECT id, agent_id, tenant_id, version, state, metadata, created_at, size
		FROM checkpoints
		WHERE agent_id = $1 AND version = $2
	`

	var checkpoint Checkpoint
	var stateJSON, metadataJSON []byte

	err := cm.db.QueryRowContext(ctx, query, agentID, version).Scan(
		&checkpoint.ID,
		&checkpoint.AgentID,
		&checkpoint.TenantID,
		&checkpoint.Version,
		&stateJSON,
		&metadataJSON,
		&checkpoint.CreatedAt,
		&checkpoint.Size,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("checkpoint version %d not found for agent %s", version, agentID)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get checkpoint: %w", err)
	}

	// Deserialize state
	if err := json.Unmarshal(stateJSON, &checkpoint.State); err != nil {
		return nil, fmt.Errorf("failed to unmarshal state: %w", err)
	}

	// Deserialize metadata
	if err := json.Unmarshal(metadataJSON, &checkpoint.Metadata); err != nil {
		return nil, fmt.Errorf("failed to unmarshal metadata: %w", err)
	}

	return &checkpoint, nil
}

// ListCheckpoints lists all checkpoints for an agent.
func (cm *CheckpointManager) ListCheckpoints(ctx context.Context, agentID api.AgentID) ([]*Checkpoint, error) {
	query := `
		SELECT id, agent_id, tenant_id, version, created_at, size
		FROM checkpoints
		WHERE agent_id = $1
		ORDER BY version DESC
	`

	rows, err := cm.db.QueryContext(ctx, query, agentID)
	if err != nil {
		return nil, fmt.Errorf("failed to list checkpoints: %w", err)
	}
	defer rows.Close()

	checkpoints := make([]*Checkpoint, 0)
	for rows.Next() {
		var cp Checkpoint
		if err := rows.Scan(&cp.ID, &cp.AgentID, &cp.TenantID, &cp.Version, &cp.CreatedAt, &cp.Size); err != nil {
			return nil, fmt.Errorf("failed to scan checkpoint: %w", err)
		}
		checkpoints = append(checkpoints, &cp)
	}

	return checkpoints, rows.Err()
}

// DeleteCheckpoint deletes a specific checkpoint.
func (cm *CheckpointManager) DeleteCheckpoint(ctx context.Context, agentID api.AgentID, version int) error {
	query := `DELETE FROM checkpoints WHERE agent_id = $1 AND version = $2`

	result, err := cm.db.ExecContext(ctx, query, agentID, version)
	if err != nil {
		return fmt.Errorf("failed to delete checkpoint: %w", err)
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("checkpoint not found")
	}

	cm.logger.InfoContext(ctx, "checkpoint deleted", "agent_id", agentID, "version", version)
	return nil
}

// RecoveryManager handles agent recovery from checkpoints.
type RecoveryManager struct {
	logger            *slog.Logger
	checkpointManager *CheckpointManager
	config            RecoveryConfig
}

// RecoveryConfig holds recovery configuration.
type RecoveryConfig struct {
	// MaxRetries for recovery attempts
	MaxRetries int

	// RetryDelay between recovery attempts
	RetryDelay time.Duration

	// HealthCheckInterval for monitoring agents
	HealthCheckInterval time.Duration
}

// DefaultRecoveryConfig returns default recovery configuration.
func DefaultRecoveryConfig() RecoveryConfig {
	return RecoveryConfig{
		MaxRetries:          3,
		RetryDelay:          30 * time.Second,
		HealthCheckInterval: 1 * time.Minute,
	}
}

// NewRecoveryManager creates a new recovery manager.
func NewRecoveryManager(logger *slog.Logger, checkpointManager *CheckpointManager, config RecoveryConfig) *RecoveryManager {
	return &RecoveryManager{
		logger:            logger.With("component", "recovery_manager"),
		checkpointManager: checkpointManager,
		config:            config,
	}
}

// RecoverAgent recovers an agent from the latest checkpoint.
func (rm *RecoveryManager) RecoverAgent(ctx context.Context, agentID api.AgentID) (map[string]interface{}, error) {
	// Get latest checkpoint
	checkpoint, err := rm.checkpointManager.GetLatestCheckpoint(ctx, agentID)
	if err != nil {
		return nil, fmt.Errorf("failed to get checkpoint: %w", err)
	}

	rm.logger.InfoContext(ctx, "recovering agent from checkpoint",
		"agent_id", agentID,
		"version", checkpoint.Version,
		"timestamp", checkpoint.CreatedAt,
	)

	// Return the state for restoration
	return checkpoint.State, nil
}

// RecoverAgentWithRetry recovers an agent with automatic retries.
func (rm *RecoveryManager) RecoverAgentWithRetry(ctx context.Context, agentID api.AgentID, restoreFunc func(map[string]interface{}) error) error {
	var lastErr error

	for attempt := 1; attempt <= rm.config.MaxRetries; attempt++ {
		rm.logger.InfoContext(ctx, "recovery attempt",
			"agent_id", agentID,
			"attempt", attempt,
			"max_retries", rm.config.MaxRetries,
		)

		// Get state from checkpoint
		state, err := rm.RecoverAgent(ctx, agentID)
		if err != nil {
			lastErr = err
			rm.logger.ErrorContext(ctx, "failed to get checkpoint", "error", err)

			if attempt < rm.config.MaxRetries {
				time.Sleep(rm.config.RetryDelay)
				continue
			}
			break
		}

		// Restore state
		if err := restoreFunc(state); err != nil {
			lastErr = err
			rm.logger.ErrorContext(ctx, "failed to restore state", "error", err)

			if attempt < rm.config.MaxRetries {
				time.Sleep(rm.config.RetryDelay)
				continue
			}
			break
		}

		// Success
		rm.logger.InfoContext(ctx, "agent recovered successfully", "agent_id", agentID)
		return nil
	}

	return fmt.Errorf("recovery failed after %d attempts: %w", rm.config.MaxRetries, lastErr)
}

// RecoveryStrategy defines how to recover an agent.
type RecoveryStrategy string

const (
	// StrategyRestart restarts the agent with last known state
	StrategyRestart RecoveryStrategy = "restart"

	// StrategyFailover fails over to a backup agent
	StrategyFailover RecoveryStrategy = "failover"

	// StrategyRollback rolls back to a previous checkpoint
	StrategyRollback RecoveryStrategy = "rollback"
)
