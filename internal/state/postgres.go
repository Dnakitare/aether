// Package state provides state persistence with PostgreSQL.
package state

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	_ "github.com/lib/pq"

	"github.com/aether-runtime/aether/pkg/api"
)

// PostgresStore provides durable state persistence using PostgreSQL.
type PostgresStore struct {
	logger *slog.Logger
	db     *sql.DB
}

// PostgresConfig holds PostgreSQL configuration.
type PostgresConfig struct {
	// DSN is the connection string (e.g., "postgres://user:pass@localhost/db")
	DSN string

	// MaxOpenConns is the maximum number of open connections
	MaxOpenConns int

	// MaxIdleConns is the maximum number of idle connections
	MaxIdleConns int

	// ConnMaxLifetime is the maximum lifetime of a connection
	ConnMaxLifetime time.Duration
}

// NewPostgresStore creates a new PostgreSQL store.
func NewPostgresStore(logger *slog.Logger, config PostgresConfig) (*PostgresStore, error) {
	if config.DSN == "" {
		return nil, fmt.Errorf("postgres DSN is required")
	}

	// Set defaults optimized for production workload
	// Allow ~100 concurrent connections for high traffic scenarios
	if config.MaxOpenConns == 0 {
		config.MaxOpenConns = 100
	}
	if config.MaxIdleConns == 0 {
		config.MaxIdleConns = 25 // 25% of max for warm pool
	}
	if config.ConnMaxLifetime == 0 {
		config.ConnMaxLifetime = 15 * time.Minute // Balance reuse vs staleness
	}

	// Open connection
	db, err := sql.Open("postgres", config.DSN)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Configure connection pool
	db.SetMaxOpenConns(config.MaxOpenConns)
	db.SetMaxIdleConns(config.MaxIdleConns)
	db.SetConnMaxLifetime(config.ConnMaxLifetime)

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return &PostgresStore{
		logger: logger.With("component", "postgres_store"),
		db:     db,
	}, nil
}

// Close closes the database connection.
func (ps *PostgresStore) Close() error {
	return ps.db.Close()
}

// CreateAgent creates a new agent record in the database.
func (ps *PostgresStore) CreateAgent(ctx context.Context, config api.AgentConfig) error {
	configJSON, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal agent config: %w", err)
	}

	query := `
		INSERT INTO agents (id, tenant_id, name, image, status, config, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`

	now := time.Now()
	_, err = ps.db.ExecContext(ctx, query,
		config.ID,
		config.TenantID,
		config.Name,
		config.Image,
		api.AgentStatusPending,
		configJSON,
		now,
		now,
	)

	if err != nil {
		return fmt.Errorf("failed to create agent: %w", err)
	}

	ps.logger.InfoContext(ctx, "agent created in database",
		"agent_id", config.ID,
		"tenant_id", config.TenantID,
		"name", config.Name,
	)

	return nil
}

// GetAgent retrieves an agent by ID.
func (ps *PostgresStore) GetAgent(ctx context.Context, agentID api.AgentID) (*api.AgentInfo, error) {
	query := `
		SELECT id, tenant_id, name, image, status, config, created_at, updated_at, started_at, stopped_at, error
		FROM agents
		WHERE id = $1
	`

	var info api.AgentInfo
	var configJSON []byte
	var startedAt, stoppedAt sql.NullTime
	var errMsg sql.NullString

	var updatedAt time.Time
	err := ps.db.QueryRowContext(ctx, query, agentID).Scan(
		&info.Config.ID,
		&info.Config.TenantID,
		&info.Config.Name,
		&info.Config.Image,
		&info.Status,
		&configJSON,
		&info.CreatedAt,
		&updatedAt,
		&startedAt,
		&stoppedAt,
		&errMsg,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("agent not found: %s", agentID)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query agent: %w", err)
	}

	// Unmarshal full config
	if err := json.Unmarshal(configJSON, &info.Config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal agent config: %w", err)
	}

	// Set optional fields
	if startedAt.Valid {
		info.StartedAt = &startedAt.Time
	}
	if stoppedAt.Valid {
		info.StoppedAt = &stoppedAt.Time
	}
	if errMsg.Valid {
		info.Error = errMsg.String
	}

	return &info, nil
}

// ListAgents retrieves all agents for a tenant.
func (ps *PostgresStore) ListAgents(ctx context.Context, tenantID api.TenantID) ([]*api.AgentInfo, error) {
	query := `
		SELECT id, tenant_id, name, image, status, config, created_at, updated_at, started_at, stopped_at, error
		FROM agents
		WHERE tenant_id = $1
		ORDER BY created_at DESC
	`

	rows, err := ps.db.QueryContext(ctx, query, tenantID)
	if err != nil {
		return nil, fmt.Errorf("failed to query agents: %w", err)
	}
	defer rows.Close()

	var agents []*api.AgentInfo

	for rows.Next() {
		var info api.AgentInfo
		var configJSON []byte
		var startedAt, stoppedAt sql.NullTime
		var errMsg sql.NullString
		var updatedAt time.Time

		err := rows.Scan(
			&info.Config.ID,
			&info.Config.TenantID,
			&info.Config.Name,
			&info.Config.Image,
			&info.Status,
			&configJSON,
			&info.CreatedAt,
			&updatedAt,
			&startedAt,
			&stoppedAt,
			&errMsg,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan agent row: %w", err)
		}

		// Unmarshal full config
		if err := json.Unmarshal(configJSON, &info.Config); err != nil {
			return nil, fmt.Errorf("failed to unmarshal agent config: %w", err)
		}

		// Set optional fields
		if startedAt.Valid {
			info.StartedAt = &startedAt.Time
		}
		if stoppedAt.Valid {
			info.StoppedAt = &stoppedAt.Time
		}
		if errMsg.Valid {
			info.Error = errMsg.String
		}

		agents = append(agents, &info)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating agents: %w", err)
	}

	ps.logger.DebugContext(ctx, "listed agents",
		"tenant_id", tenantID,
		"count", len(agents),
	)

	return agents, nil
}

// UpdateAgentStatus updates an agent's status and related timestamps.
func (ps *PostgresStore) UpdateAgentStatus(ctx context.Context, agentID api.AgentID, status api.AgentStatus) error {
	now := time.Now()

	// Build dynamic query based on status
	var query string
	var args []interface{}

	switch status {
	case api.AgentStatusRunning:
		query = `
			UPDATE agents
			SET status = $1, started_at = $2, updated_at = $3
			WHERE id = $4
		`
		args = []interface{}{status, now, now, agentID}

	case api.AgentStatusStopped, api.AgentStatusFailed:
		query = `
			UPDATE agents
			SET status = $1, stopped_at = $2, updated_at = $3
			WHERE id = $4
		`
		args = []interface{}{status, now, now, agentID}

	default:
		query = `
			UPDATE agents
			SET status = $1, updated_at = $2
			WHERE id = $3
		`
		args = []interface{}{status, now, agentID}
	}

	result, err := ps.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("failed to update agent status: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rows == 0 {
		return fmt.Errorf("agent not found: %s", agentID)
	}

	ps.logger.InfoContext(ctx, "agent status updated",
		"agent_id", agentID,
		"status", status,
	)

	return nil
}

// SetAgentError sets an error message on an agent and marks it as failed.
func (ps *PostgresStore) SetAgentError(ctx context.Context, agentID api.AgentID, errMsg string) error {
	now := time.Now()

	query := `
		UPDATE agents
		SET status = $1, error = $2, stopped_at = $3, updated_at = $4
		WHERE id = $5
	`

	result, err := ps.db.ExecContext(ctx, query, api.AgentStatusFailed, errMsg, now, now, agentID)
	if err != nil {
		return fmt.Errorf("failed to set agent error: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rows == 0 {
		return fmt.Errorf("agent not found: %s", agentID)
	}

	ps.logger.ErrorContext(ctx, "agent error recorded",
		"agent_id", agentID,
		"error", errMsg,
	)

	return nil
}

// DeleteAgent deletes an agent from the database.
func (ps *PostgresStore) DeleteAgent(ctx context.Context, agentID api.AgentID) error {
	query := `DELETE FROM agents WHERE id = $1`

	result, err := ps.db.ExecContext(ctx, query, agentID)
	if err != nil {
		return fmt.Errorf("failed to delete agent: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rows == 0 {
		return fmt.Errorf("agent not found: %s", agentID)
	}

	ps.logger.InfoContext(ctx, "agent deleted from database",
		"agent_id", agentID,
	)

	return nil
}

// GetAgentsByStatus retrieves all agents with a specific status.
func (ps *PostgresStore) GetAgentsByStatus(ctx context.Context, status api.AgentStatus) ([]*api.AgentInfo, error) {
	query := `
		SELECT id, tenant_id, name, image, status, config, created_at, updated_at, started_at, stopped_at, error
		FROM agents
		WHERE status = $1
		ORDER BY created_at DESC
	`

	rows, err := ps.db.QueryContext(ctx, query, status)
	if err != nil {
		return nil, fmt.Errorf("failed to query agents by status: %w", err)
	}
	defer rows.Close()

	var agents []*api.AgentInfo

	for rows.Next() {
		var info api.AgentInfo
		var configJSON []byte
		var startedAt, stoppedAt sql.NullTime
		var errMsg sql.NullString
		var updatedAt time.Time

		err := rows.Scan(
			&info.Config.ID,
			&info.Config.TenantID,
			&info.Config.Name,
			&info.Config.Image,
			&info.Status,
			&configJSON,
			&info.CreatedAt,
			&updatedAt,
			&startedAt,
			&stoppedAt,
			&errMsg,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan agent row: %w", err)
		}

		// Unmarshal full config
		if err := json.Unmarshal(configJSON, &info.Config); err != nil {
			return nil, fmt.Errorf("failed to unmarshal agent config: %w", err)
		}

		// Set optional fields
		if startedAt.Valid {
			info.StartedAt = &startedAt.Time
		}
		if stoppedAt.Valid {
			info.StoppedAt = &stoppedAt.Time
		}
		if errMsg.Valid {
			info.Error = errMsg.String
		}

		agents = append(agents, &info)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating agents: %w", err)
	}

	return agents, nil
}

// GetTenantAgentCount returns the number of agents for a tenant.
func (ps *PostgresStore) GetTenantAgentCount(ctx context.Context, tenantID api.TenantID) (int, error) {
	query := `SELECT COUNT(*) FROM agents WHERE tenant_id = $1`

	var count int
	err := ps.db.QueryRowContext(ctx, query, tenantID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count tenant agents: %w", err)
	}

	return count, nil
}
