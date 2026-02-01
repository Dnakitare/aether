// Package tenant provides multi-tenant resource management.
package tenant

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/dnakitare/aether/pkg/api"
)

// QuotaManager manages resource quotas for tenants.
type QuotaManager struct {
	logger *slog.Logger
	mu     sync.RWMutex

	// Quotas by tenant ID
	quotas map[api.TenantID]*Quota

	// Current usage by tenant
	usage map[api.TenantID]*ResourceUsage
}

// Quota defines resource limits for a tenant.
type Quota struct {
	TenantID api.TenantID

	// Maximum concurrent agents
	MaxAgents int

	// Maximum CPU cores (in millicores)
	MaxCPUCores int64

	// Maximum memory (in MB)
	MaxMemoryMB int64

	// Maximum disk (in MB)
	MaxDiskMB int64

	// Maximum API requests per minute
	MaxRequestsPerMinute int64

	// Tier level (free, pro, enterprise)
	Tier string

	// CreatedAt timestamp
	CreatedAt time.Time

	// UpdatedAt timestamp
	UpdatedAt time.Time
}

// ResourceUsage tracks current resource consumption.
type ResourceUsage struct {
	TenantID api.TenantID

	// Current agent count
	AgentCount int

	// Current CPU allocation (millicores)
	CPUCores int64

	// Current memory allocation (MB)
	MemoryMB int64

	// Current disk allocation (MB)
	DiskMB int64

	// API requests in current window
	RequestCount int64

	// Last updated
	LastUpdated time.Time
}

// NewQuotaManager creates a new quota manager.
func NewQuotaManager(logger *slog.Logger) *QuotaManager {
	return &QuotaManager{
		logger: logger.With("component", "quota_manager"),
		quotas: make(map[api.TenantID]*Quota),
		usage:  make(map[api.TenantID]*ResourceUsage),
	}
}

// SetQuota sets or updates a quota for a tenant.
func (qm *QuotaManager) SetQuota(quota *Quota) error {
	qm.mu.Lock()
	defer qm.mu.Unlock()

	if quota.TenantID == "" {
		return fmt.Errorf("tenant ID is required")
	}

	if quota.MaxAgents <= 0 {
		return fmt.Errorf("max agents must be > 0")
	}

	now := time.Now()
	if quota.CreatedAt.IsZero() {
		quota.CreatedAt = now
	}
	quota.UpdatedAt = now

	qm.quotas[quota.TenantID] = quota

	// Initialize usage if not exists
	if _, exists := qm.usage[quota.TenantID]; !exists {
		qm.usage[quota.TenantID] = &ResourceUsage{
			TenantID:    quota.TenantID,
			LastUpdated: now,
		}
	}

	qm.logger.InfoContext(context.Background(),
		"quota updated",
		"tenant_id", quota.TenantID,
		"max_agents", quota.MaxAgents,
		"tier", quota.Tier,
	)

	return nil
}

// GetQuota retrieves a quota for a tenant.
func (qm *QuotaManager) GetQuota(tenantID api.TenantID) (*Quota, error) {
	qm.mu.RLock()
	defer qm.mu.RUnlock()

	quota, exists := qm.quotas[tenantID]
	if !exists {
		return nil, fmt.Errorf("quota not found for tenant %s", tenantID)
	}

	return quota, nil
}

// GetUsage retrieves current usage for a tenant.
func (qm *QuotaManager) GetUsage(tenantID api.TenantID) (*ResourceUsage, error) {
	qm.mu.RLock()
	defer qm.mu.RUnlock()

	usage, exists := qm.usage[tenantID]
	if !exists {
		return nil, fmt.Errorf("usage not found for tenant %s", tenantID)
	}

	return usage, nil
}

// CheckQuota checks if a tenant can allocate the requested resources.
func (qm *QuotaManager) CheckQuota(ctx context.Context, tenantID api.TenantID, requested ResourceRequest) error {
	qm.mu.RLock()
	defer qm.mu.RUnlock()

	quota, exists := qm.quotas[tenantID]
	if !exists {
		return fmt.Errorf("no quota configured for tenant %s", tenantID)
	}

	usage, exists := qm.usage[tenantID]
	if !exists {
		return fmt.Errorf("usage tracking not initialized for tenant %s", tenantID)
	}

	// Check agent count
	if usage.AgentCount+requested.AgentCount > quota.MaxAgents {
		return &QuotaExceededError{
			TenantID:  tenantID,
			Resource:  "agents",
			Requested: int64(usage.AgentCount + requested.AgentCount),
			Limit:     int64(quota.MaxAgents),
		}
	}

	// Check CPU
	if usage.CPUCores+requested.CPUCores > quota.MaxCPUCores {
		return &QuotaExceededError{
			TenantID:  tenantID,
			Resource:  "cpu_cores",
			Requested: usage.CPUCores + requested.CPUCores,
			Limit:     quota.MaxCPUCores,
		}
	}

	// Check Memory
	if usage.MemoryMB+requested.MemoryMB > quota.MaxMemoryMB {
		return &QuotaExceededError{
			TenantID:  tenantID,
			Resource:  "memory_mb",
			Requested: usage.MemoryMB + requested.MemoryMB,
			Limit:     quota.MaxMemoryMB,
		}
	}

	// Check Disk
	if quota.MaxDiskMB > 0 && usage.DiskMB+requested.DiskMB > quota.MaxDiskMB {
		return &QuotaExceededError{
			TenantID:  tenantID,
			Resource:  "disk_mb",
			Requested: usage.DiskMB + requested.DiskMB,
			Limit:     quota.MaxDiskMB,
		}
	}

	return nil
}

// AllocateResources allocates resources for a tenant.
func (qm *QuotaManager) AllocateResources(ctx context.Context, tenantID api.TenantID, allocation ResourceRequest) error {
	qm.mu.Lock()
	defer qm.mu.Unlock()

	usage, exists := qm.usage[tenantID]
	if !exists {
		return fmt.Errorf("usage tracking not initialized for tenant %s", tenantID)
	}

	usage.AgentCount += allocation.AgentCount
	usage.CPUCores += allocation.CPUCores
	usage.MemoryMB += allocation.MemoryMB
	usage.DiskMB += allocation.DiskMB
	usage.LastUpdated = time.Now()

	qm.logger.DebugContext(ctx,
		"resources allocated",
		"tenant_id", tenantID,
		"agents", usage.AgentCount,
		"cpu_cores", usage.CPUCores,
		"memory_mb", usage.MemoryMB,
	)

	return nil
}

// ReleaseResources releases resources for a tenant.
func (qm *QuotaManager) ReleaseResources(ctx context.Context, tenantID api.TenantID, release ResourceRequest) error {
	qm.mu.Lock()
	defer qm.mu.Unlock()

	usage, exists := qm.usage[tenantID]
	if !exists {
		return fmt.Errorf("usage tracking not initialized for tenant %s", tenantID)
	}

	usage.AgentCount -= release.AgentCount
	usage.CPUCores -= release.CPUCores
	usage.MemoryMB -= release.MemoryMB
	usage.DiskMB -= release.DiskMB
	usage.LastUpdated = time.Now()

	// Ensure no negative values
	if usage.AgentCount < 0 {
		usage.AgentCount = 0
	}
	if usage.CPUCores < 0 {
		usage.CPUCores = 0
	}
	if usage.MemoryMB < 0 {
		usage.MemoryMB = 0
	}
	if usage.DiskMB < 0 {
		usage.DiskMB = 0
	}

	qm.logger.DebugContext(ctx,
		"resources released",
		"tenant_id", tenantID,
		"agents", usage.AgentCount,
		"cpu_cores", usage.CPUCores,
		"memory_mb", usage.MemoryMB,
	)

	return nil
}

// ListQuotas returns all configured quotas.
func (qm *QuotaManager) ListQuotas() []*Quota {
	qm.mu.RLock()
	defer qm.mu.RUnlock()

	quotas := make([]*Quota, 0, len(qm.quotas))
	for _, quota := range qm.quotas {
		quotas = append(quotas, quota)
	}
	return quotas
}

// ResourceRequest represents a resource allocation/release request.
type ResourceRequest struct {
	AgentCount int
	CPUCores   int64
	MemoryMB   int64
	DiskMB     int64
}

// QuotaExceededError is returned when a quota limit is exceeded.
type QuotaExceededError struct {
	TenantID  api.TenantID
	Resource  string
	Requested int64
	Limit     int64
}

func (e *QuotaExceededError) Error() string {
	return fmt.Sprintf("quota exceeded for tenant %s: %s (requested: %d, limit: %d)",
		e.TenantID, e.Resource, e.Requested, e.Limit)
}

// DefaultQuotas provides pre-configured quota tiers.
func DefaultQuotas() map[string]*Quota {
	now := time.Now()

	return map[string]*Quota{
		"free": {
			MaxAgents:            5,
			MaxCPUCores:          5000,  // 5 cores
			MaxMemoryMB:          8192,  // 8GB
			MaxDiskMB:            51200, // 50GB
			MaxRequestsPerMinute: 100,
			Tier:                 "free",
			CreatedAt:            now,
			UpdatedAt:            now,
		},
		"pro": {
			MaxAgents:            50,
			MaxCPUCores:          50000,  // 50 cores
			MaxMemoryMB:          102400, // 100GB
			MaxDiskMB:            512000, // 500GB
			MaxRequestsPerMinute: 1000,
			Tier:                 "pro",
			CreatedAt:            now,
			UpdatedAt:            now,
		},
		"enterprise": {
			MaxAgents:            500,
			MaxCPUCores:          500000,  // 500 cores
			MaxMemoryMB:          1048576, // 1TB
			MaxDiskMB:            5120000, // 5TB
			MaxRequestsPerMinute: 10000,
			Tier:                 "enterprise",
			CreatedAt:            now,
			UpdatedAt:            now,
		},
	}
}
