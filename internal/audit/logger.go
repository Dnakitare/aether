// Package audit provides immutable audit logging.
package audit

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	_ "github.com/lib/pq"

	"github.com/dnakitare/aether/pkg/api"
)

// Logger provides audit logging to PostgreSQL.
type Logger struct {
	logger *slog.Logger
	db     *sql.DB
	config Config
}

// Config holds audit logger configuration.
type Config struct {
	// PostgreSQL connection string
	// Example: "postgres://user:pass@localhost/aether?sslmode=disable"
	DSN string

	// TableName for audit logs (default: "audit_logs")
	TableName string

	// RetentionDays is how long to keep audit logs (0 = forever)
	RetentionDays int
}

// Event represents an audit event.
type Event struct {
	ID         int64
	Timestamp  time.Time
	TenantID   api.TenantID
	UserID     string
	Action     Action
	Resource   ResourceType
	ResourceID string
	Result     Result
	IPAddress  string
	UserAgent  string
	Details    map[string]interface{}
	Error      string
}

// Action represents an audited action.
type Action string

const (
	ActionCreate Action = "create"
	ActionRead   Action = "read"
	ActionUpdate Action = "update"
	ActionDelete Action = "delete"
	ActionStart  Action = "start"
	ActionStop   Action = "stop"
	ActionLogin  Action = "login"
	ActionLogout Action = "logout"
)

// ResourceType represents the type of resource being audited.
type ResourceType string

const (
	ResourceAgent    ResourceType = "agent"
	ResourceQuota    ResourceType = "quota"
	ResourcePolicy   ResourceType = "policy"
	ResourceSecret   ResourceType = "secret"
	ResourceAPIKey   ResourceType = "api_key"
	ResourceFirewall ResourceType = "firewall"
)

// Result represents the outcome of an action.
type Result string

const (
	ResultSuccess Result = "success"
	ResultFailure Result = "failure"
	ResultDenied  Result = "denied"
)

// NewLogger creates a new audit logger.
func NewLogger(logger *slog.Logger, config Config) (*Logger, error) {
	if config.DSN == "" {
		return nil, fmt.Errorf("database DSN is required")
	}

	if config.TableName == "" {
		config.TableName = "audit_logs"
	}

	// Connect to database
	db, err := sql.Open("postgres", config.DSN)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	// Test connection
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	al := &Logger{
		logger: logger.With("component", "audit_logger"),
		db:     db,
		config: config,
	}

	// Initialize database schema
	if err := al.initSchema(context.Background()); err != nil {
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	return al, nil
}

// Log logs an audit event.
func (al *Logger) Log(ctx context.Context, event *Event) error {
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}

	// Serialize details to JSON
	var detailsJSON []byte
	var err error
	if event.Details != nil {
		detailsJSON, err = json.Marshal(event.Details)
		if err != nil {
			return fmt.Errorf("failed to marshal details: %w", err)
		}
	}

	query := fmt.Sprintf(`
		INSERT INTO %s (
			timestamp, tenant_id, user_id, action, resource, resource_id,
			result, ip_address, user_agent, details, error
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		RETURNING id
	`, al.config.TableName)

	err = al.db.QueryRowContext(
		ctx,
		query,
		event.Timestamp,
		event.TenantID,
		event.UserID,
		event.Action,
		event.Resource,
		event.ResourceID,
		event.Result,
		event.IPAddress,
		event.UserAgent,
		detailsJSON,
		event.Error,
	).Scan(&event.ID)

	if err != nil {
		return fmt.Errorf("failed to insert audit log: %w", err)
	}

	al.logger.DebugContext(ctx,
		"audit event logged",
		"id", event.ID,
		"action", event.Action,
		"resource", event.Resource,
		"result", event.Result,
	)

	return nil
}

// Query queries audit logs with filters.
func (al *Logger) Query(ctx context.Context, filters QueryFilters) ([]*Event, error) {
	query := fmt.Sprintf(`
		SELECT id, timestamp, tenant_id, user_id, action, resource, resource_id,
		       result, ip_address, user_agent, details, error
		FROM %s
		WHERE 1=1
	`, al.config.TableName)

	args := make([]interface{}, 0)
	argNum := 1

	if filters.TenantID != "" {
		query += fmt.Sprintf(" AND tenant_id = $%d", argNum)
		args = append(args, filters.TenantID)
		argNum++
	}

	if filters.UserID != "" {
		query += fmt.Sprintf(" AND user_id = $%d", argNum)
		args = append(args, filters.UserID)
		argNum++
	}

	if filters.Action != "" {
		query += fmt.Sprintf(" AND action = $%d", argNum)
		args = append(args, filters.Action)
		argNum++
	}

	if filters.Resource != "" {
		query += fmt.Sprintf(" AND resource = $%d", argNum)
		args = append(args, filters.Resource)
		argNum++
	}

	if filters.ResourceID != "" {
		query += fmt.Sprintf(" AND resource_id = $%d", argNum)
		args = append(args, filters.ResourceID)
		argNum++
	}

	if !filters.StartTime.IsZero() {
		query += fmt.Sprintf(" AND timestamp >= $%d", argNum)
		args = append(args, filters.StartTime)
		argNum++
	}

	if !filters.EndTime.IsZero() {
		query += fmt.Sprintf(" AND timestamp <= $%d", argNum)
		args = append(args, filters.EndTime)
		argNum++
	}

	query += " ORDER BY timestamp DESC"

	if filters.Limit > 0 {
		query += fmt.Sprintf(" LIMIT $%d", argNum)
		args = append(args, filters.Limit)
		argNum++
	}

	if filters.Offset > 0 {
		query += fmt.Sprintf(" OFFSET $%d", argNum)
		args = append(args, filters.Offset)
	}

	rows, err := al.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query audit logs: %w", err)
	}
	defer rows.Close()

	events := make([]*Event, 0)

	for rows.Next() {
		event := &Event{}
		var detailsJSON []byte

		err := rows.Scan(
			&event.ID,
			&event.Timestamp,
			&event.TenantID,
			&event.UserID,
			&event.Action,
			&event.Resource,
			&event.ResourceID,
			&event.Result,
			&event.IPAddress,
			&event.UserAgent,
			&detailsJSON,
			&event.Error,
		)

		if err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}

		if len(detailsJSON) > 0 {
			if err := json.Unmarshal(detailsJSON, &event.Details); err != nil {
				al.logger.WarnContext(ctx, "failed to unmarshal details", "error", err)
			}
		}

		events = append(events, event)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	return events, nil
}

// Count counts audit logs matching filters.
func (al *Logger) Count(ctx context.Context, filters QueryFilters) (int64, error) {
	query := fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE 1=1", al.config.TableName)

	args := make([]interface{}, 0)
	argNum := 1

	if filters.TenantID != "" {
		query += fmt.Sprintf(" AND tenant_id = $%d", argNum)
		args = append(args, filters.TenantID)
		argNum++
	}

	if filters.Action != "" {
		query += fmt.Sprintf(" AND action = $%d", argNum)
		args = append(args, filters.Action)
		argNum++
	}

	if !filters.StartTime.IsZero() {
		query += fmt.Sprintf(" AND timestamp >= $%d", argNum)
		args = append(args, filters.StartTime)
		argNum++
	}

	if !filters.EndTime.IsZero() {
		query += fmt.Sprintf(" AND timestamp <= $%d", argNum)
		args = append(args, filters.EndTime)
	}

	var count int64
	err := al.db.QueryRowContext(ctx, query, args...).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count audit logs: %w", err)
	}

	return count, nil
}

// CleanupOld removes audit logs older than retention period.
func (al *Logger) CleanupOld(ctx context.Context) (int64, error) {
	if al.config.RetentionDays == 0 {
		return 0, nil // Retention disabled
	}

	cutoff := time.Now().AddDate(0, 0, -al.config.RetentionDays)

	query := fmt.Sprintf("DELETE FROM %s WHERE timestamp < $1", al.config.TableName)

	result, err := al.db.ExecContext(ctx, query, cutoff)
	if err != nil {
		return 0, fmt.Errorf("failed to cleanup old logs: %w", err)
	}

	deleted, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("failed to get rows affected: %w", err)
	}

	if deleted > 0 {
		al.logger.InfoContext(ctx, "cleaned up old audit logs", "deleted", deleted, "cutoff", cutoff)
	}

	return deleted, nil
}

// Close closes the database connection.
func (al *Logger) Close() error {
	return al.db.Close()
}

// initSchema creates the audit logs table if it doesn't exist.
func (al *Logger) initSchema(ctx context.Context) error {
	query := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			id BIGSERIAL PRIMARY KEY,
			timestamp TIMESTAMP WITH TIME ZONE NOT NULL,
			tenant_id VARCHAR(255) NOT NULL,
			user_id VARCHAR(255) NOT NULL,
			action VARCHAR(50) NOT NULL,
			resource VARCHAR(50) NOT NULL,
			resource_id VARCHAR(255),
			result VARCHAR(20) NOT NULL,
			ip_address VARCHAR(45),
			user_agent TEXT,
			details JSONB,
			error TEXT,
			created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
		);

		CREATE INDEX IF NOT EXISTS idx_%s_timestamp ON %s (timestamp DESC);
		CREATE INDEX IF NOT EXISTS idx_%s_tenant_id ON %s (tenant_id);
		CREATE INDEX IF NOT EXISTS idx_%s_user_id ON %s (user_id);
		CREATE INDEX IF NOT EXISTS idx_%s_action ON %s (action);
		CREATE INDEX IF NOT EXISTS idx_%s_resource ON %s (resource);
		CREATE INDEX IF NOT EXISTS idx_%s_result ON %s (result);
	`, al.config.TableName,
		al.config.TableName, al.config.TableName,
		al.config.TableName, al.config.TableName,
		al.config.TableName, al.config.TableName,
		al.config.TableName, al.config.TableName,
		al.config.TableName, al.config.TableName,
		al.config.TableName, al.config.TableName,
	)

	_, err := al.db.ExecContext(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to create schema: %w", err)
	}

	return nil
}

// QueryFilters defines filters for querying audit logs.
type QueryFilters struct {
	TenantID   api.TenantID
	UserID     string
	Action     Action
	Resource   ResourceType
	ResourceID string
	StartTime  time.Time
	EndTime    time.Time
	Limit      int
	Offset     int
}
