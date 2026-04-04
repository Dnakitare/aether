// Package tenant provides multi-tenant resource management.
package tenant

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/dnakitare/aether/pkg/api"
)

// QuotaStore persists quota limits and usage across restarts.
type QuotaStore interface {
	// UpsertQuota creates or updates a tenant's quota limits.
	UpsertQuota(ctx context.Context, q *Quota) error
	// UpsertUsage creates or updates a tenant's current resource usage.
	UpsertUsage(ctx context.Context, tenantID api.TenantID, u *ResourceUsage) error
	// LoadAll loads all quotas and usage records from storage.
	LoadAll(ctx context.Context) ([]*Quota, []*ResourceUsage, error)
}

// PostgresQuotaStore implements QuotaStore using PostgreSQL.
type PostgresQuotaStore struct {
	db *sql.DB
}

// NewPostgresQuotaStore creates a PostgreSQL-backed quota store.
func NewPostgresQuotaStore(db *sql.DB) *PostgresQuotaStore {
	return &PostgresQuotaStore{db: db}
}

// UpsertQuota writes a tenant's quota limits to the database.
func (s *PostgresQuotaStore) UpsertQuota(ctx context.Context, q *Quota) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO quotas (tenant_id, max_agents, max_cpu_cores, max_memory_mb, max_disk_mb, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (tenant_id) DO UPDATE SET
			max_agents     = EXCLUDED.max_agents,
			max_cpu_cores  = EXCLUDED.max_cpu_cores,
			max_memory_mb  = EXCLUDED.max_memory_mb,
			max_disk_mb    = EXCLUDED.max_disk_mb,
			updated_at     = EXCLUDED.updated_at
	`, q.TenantID, q.MaxAgents, q.MaxCPUCores, q.MaxMemoryMB, q.MaxDiskMB, q.UpdatedAt)
	if err != nil {
		return fmt.Errorf("failed to upsert quota: %w", err)
	}

	// Ensure a usage row exists so later UpsertUsage calls don't need to
	// worry about the row being absent.
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO quota_usage (tenant_id, current_agents, current_cpu_cores, current_memory_mb, current_disk_mb, updated_at)
		VALUES ($1, 0, 0, 0, 0, $2)
		ON CONFLICT (tenant_id) DO NOTHING
	`, q.TenantID, time.Now())
	if err != nil {
		return fmt.Errorf("failed to initialise quota usage row: %w", err)
	}

	return nil
}

// UpsertUsage writes a tenant's current resource usage to the database.
func (s *PostgresQuotaStore) UpsertUsage(ctx context.Context, tenantID api.TenantID, u *ResourceUsage) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO quota_usage (tenant_id, current_agents, current_cpu_cores, current_memory_mb, current_disk_mb, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (tenant_id) DO UPDATE SET
			current_agents    = EXCLUDED.current_agents,
			current_cpu_cores = EXCLUDED.current_cpu_cores,
			current_memory_mb = EXCLUDED.current_memory_mb,
			current_disk_mb   = EXCLUDED.current_disk_mb,
			updated_at        = EXCLUDED.updated_at
	`, tenantID, u.AgentCount, u.CPUCores, u.MemoryMB, u.DiskMB, u.LastUpdated)
	if err != nil {
		return fmt.Errorf("failed to upsert quota usage: %w", err)
	}
	return nil
}

// LoadAll loads all quota limits and usage records from the database.
func (s *PostgresQuotaStore) LoadAll(ctx context.Context) ([]*Quota, []*ResourceUsage, error) {
	quotaRows, err := s.db.QueryContext(ctx, `
		SELECT tenant_id, max_agents, max_cpu_cores, max_memory_mb, max_disk_mb, created_at, updated_at
		FROM quotas
	`)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to query quotas: %w", err)
	}
	defer quotaRows.Close()

	var quotas []*Quota
	for quotaRows.Next() {
		q := &Quota{}
		if err := quotaRows.Scan(
			&q.TenantID, &q.MaxAgents, &q.MaxCPUCores, &q.MaxMemoryMB, &q.MaxDiskMB,
			&q.CreatedAt, &q.UpdatedAt,
		); err != nil {
			return nil, nil, fmt.Errorf("failed to scan quota row: %w", err)
		}
		quotas = append(quotas, q)
	}
	if err := quotaRows.Err(); err != nil {
		return nil, nil, fmt.Errorf("error iterating quota rows: %w", err)
	}

	usageRows, err := s.db.QueryContext(ctx, `
		SELECT tenant_id, current_agents, current_cpu_cores, current_memory_mb, current_disk_mb, updated_at
		FROM quota_usage
	`)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to query quota_usage: %w", err)
	}
	defer usageRows.Close()

	var usages []*ResourceUsage
	for usageRows.Next() {
		u := &ResourceUsage{}
		if err := usageRows.Scan(
			&u.TenantID, &u.AgentCount, &u.CPUCores, &u.MemoryMB, &u.DiskMB,
			&u.LastUpdated,
		); err != nil {
			return nil, nil, fmt.Errorf("failed to scan usage row: %w", err)
		}
		usages = append(usages, u)
	}
	if err := usageRows.Err(); err != nil {
		return nil, nil, fmt.Errorf("error iterating usage rows: %w", err)
	}

	return quotas, usages, nil
}
