package observability

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"github.com/aether-runtime/aether/pkg/api"
)

// CostTracker tracks resource usage and calculates costs.
type CostTracker struct {
	logger  *slog.Logger
	db      *sql.DB
	pricing PricingConfig
	metrics *MetricsCollector
}

// PricingConfig holds pricing information for resources.
type PricingConfig struct {
	// CPU cost per core-hour in dollars
	CPUCoreHour float64

	// Memory cost per GB-hour in dollars
	MemoryGBHour float64

	// Storage cost per GB-month in dollars
	StorageGBMonth float64

	// Network cost per GB transferred
	NetworkGB float64
}

// DefaultPricing returns default pricing configuration.
func DefaultPricing() PricingConfig {
	return PricingConfig{
		CPUCoreHour:    0.04,  // $0.04 per core-hour
		MemoryGBHour:   0.005, // $0.005 per GB-hour
		StorageGBMonth: 0.10,  // $0.10 per GB-month
		NetworkGB:      0.09,  // $0.09 per GB transferred
	}
}

// CostRecord represents a cost entry.
type CostRecord struct {
	ID           int64
	Timestamp    time.Time
	TenantID     api.TenantID
	AgentID      api.AgentID
	ResourceType string
	Amount       float64 // Resource usage amount
	Cost         float64 // Cost in dollars
	Period       time.Duration
}

// CostSummary aggregates costs for a time period.
type CostSummary struct {
	TenantID      api.TenantID
	StartTime     time.Time
	EndTime       time.Time
	CPUCost       float64
	MemoryCost    float64
	StorageCost   float64
	NetworkCost   float64
	TotalCost     float64
	AgentCount    int
	CPUCoreHours  float64
	MemoryGBHours float64
}

// NewCostTracker creates a new cost tracker.
func NewCostTracker(logger *slog.Logger, db *sql.DB, pricing PricingConfig, metrics *MetricsCollector) (*CostTracker, error) {
	ct := &CostTracker{
		logger:  logger.With("component", "cost_tracker"),
		db:      db,
		pricing: pricing,
		metrics: metrics,
	}

	// Create costs table
	if err := ct.createTable(); err != nil {
		return nil, err
	}

	return ct, nil
}

func (ct *CostTracker) createTable() error {
	query := `
	CREATE TABLE IF NOT EXISTS costs (
		id BIGSERIAL PRIMARY KEY,
		timestamp TIMESTAMP WITH TIME ZONE NOT NULL,
		tenant_id VARCHAR(255) NOT NULL,
		agent_id VARCHAR(255),
		resource_type VARCHAR(50) NOT NULL,
		amount DOUBLE PRECISION NOT NULL,
		cost DOUBLE PRECISION NOT NULL,
		period_seconds BIGINT NOT NULL,
		created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
	);

	CREATE INDEX IF NOT EXISTS idx_costs_timestamp ON costs (timestamp DESC);
	CREATE INDEX IF NOT EXISTS idx_costs_tenant_id ON costs (tenant_id);
	CREATE INDEX IF NOT EXISTS idx_costs_agent_id ON costs (agent_id);
	`

	if _, err := ct.db.Exec(query); err != nil {
		return fmt.Errorf("failed to create costs table: %w", err)
	}

	return nil
}

// RecordCPUUsage records CPU usage and calculates cost.
func (ct *CostTracker) RecordCPUUsage(ctx context.Context, tenantID api.TenantID, agentID api.AgentID, cores float64, duration time.Duration) error {
	hours := duration.Hours()
	coreHours := cores * hours
	cost := coreHours * ct.pricing.CPUCoreHour

	record := &CostRecord{
		Timestamp:    time.Now(),
		TenantID:     tenantID,
		AgentID:      agentID,
		ResourceType: "cpu",
		Amount:       coreHours,
		Cost:         cost,
		Period:       duration,
	}

	if err := ct.saveCostRecord(ctx, record); err != nil {
		return err
	}

	// Update metrics
	if ct.metrics != nil {
		ct.metrics.RecordResourceCost("cpu", tenantID, cost)
	}

	ct.logger.DebugContext(ctx, "recorded CPU cost",
		"tenant_id", tenantID,
		"agent_id", agentID,
		"core_hours", coreHours,
		"cost", cost,
	)

	return nil
}

// RecordMemoryUsage records memory usage and calculates cost.
func (ct *CostTracker) RecordMemoryUsage(ctx context.Context, tenantID api.TenantID, agentID api.AgentID, memoryGB float64, duration time.Duration) error {
	hours := duration.Hours()
	gbHours := memoryGB * hours
	cost := gbHours * ct.pricing.MemoryGBHour

	record := &CostRecord{
		Timestamp:    time.Now(),
		TenantID:     tenantID,
		AgentID:      agentID,
		ResourceType: "memory",
		Amount:       gbHours,
		Cost:         cost,
		Period:       duration,
	}

	if err := ct.saveCostRecord(ctx, record); err != nil {
		return err
	}

	// Update metrics
	if ct.metrics != nil {
		ct.metrics.RecordResourceCost("memory", tenantID, cost)
	}

	ct.logger.DebugContext(ctx, "recorded memory cost",
		"tenant_id", tenantID,
		"agent_id", agentID,
		"gb_hours", gbHours,
		"cost", cost,
	)

	return nil
}

func (ct *CostTracker) saveCostRecord(ctx context.Context, record *CostRecord) error {
	query := `
		INSERT INTO costs (timestamp, tenant_id, agent_id, resource_type, amount, cost, period_seconds)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id
	`

	err := ct.db.QueryRowContext(
		ctx,
		query,
		record.Timestamp,
		record.TenantID,
		record.AgentID,
		record.ResourceType,
		record.Amount,
		record.Cost,
		int64(record.Period.Seconds()),
	).Scan(&record.ID)

	if err != nil {
		return fmt.Errorf("failed to save cost record: %w", err)
	}

	return nil
}

// GetCostSummary retrieves cost summary for a tenant in a time range.
func (ct *CostTracker) GetCostSummary(ctx context.Context, tenantID api.TenantID, startTime, endTime time.Time) (*CostSummary, error) {
	query := `
		SELECT
			COALESCE(SUM(CASE WHEN resource_type = 'cpu' THEN cost ELSE 0 END), 0) as cpu_cost,
			COALESCE(SUM(CASE WHEN resource_type = 'memory' THEN cost ELSE 0 END), 0) as memory_cost,
			COALESCE(SUM(CASE WHEN resource_type = 'storage' THEN cost ELSE 0 END), 0) as storage_cost,
			COALESCE(SUM(CASE WHEN resource_type = 'network' THEN cost ELSE 0 END), 0) as network_cost,
			COALESCE(SUM(cost), 0) as total_cost,
			COUNT(DISTINCT agent_id) as agent_count,
			COALESCE(SUM(CASE WHEN resource_type = 'cpu' THEN amount ELSE 0 END), 0) as cpu_core_hours,
			COALESCE(SUM(CASE WHEN resource_type = 'memory' THEN amount ELSE 0 END), 0) as memory_gb_hours
		FROM costs
		WHERE tenant_id = $1
		AND timestamp >= $2
		AND timestamp <= $3
	`

	var summary CostSummary
	summary.TenantID = tenantID
	summary.StartTime = startTime
	summary.EndTime = endTime

	err := ct.db.QueryRowContext(ctx, query, tenantID, startTime, endTime).Scan(
		&summary.CPUCost,
		&summary.MemoryCost,
		&summary.StorageCost,
		&summary.NetworkCost,
		&summary.TotalCost,
		&summary.AgentCount,
		&summary.CPUCoreHours,
		&summary.MemoryGBHours,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to get cost summary: %w", err)
	}

	return &summary, nil
}

// DetectCostAnomalies detects unusual cost spikes.
func (ct *CostTracker) DetectCostAnomalies(ctx context.Context, tenantID api.TenantID, threshold float64) ([]CostAnomaly, error) {
	// Get hourly costs for the last 24 hours
	endTime := time.Now()
	startTime := endTime.Add(-24 * time.Hour)

	query := `
		SELECT
			DATE_TRUNC('hour', timestamp) as hour,
			SUM(cost) as hourly_cost
		FROM costs
		WHERE tenant_id = $1
		AND timestamp >= $2
		AND timestamp <= $3
		GROUP BY hour
		ORDER BY hour DESC
	`

	rows, err := ct.db.QueryContext(ctx, query, tenantID, startTime, endTime)
	if err != nil {
		return nil, fmt.Errorf("failed to query hourly costs: %w", err)
	}
	defer rows.Close()

	var hourlyCosts []float64
	var timestamps []time.Time

	for rows.Next() {
		var hour time.Time
		var cost float64
		if err := rows.Scan(&hour, &cost); err != nil {
			return nil, fmt.Errorf("failed to scan hourly cost: %w", err)
		}
		timestamps = append(timestamps, hour)
		hourlyCosts = append(hourlyCosts, cost)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Calculate average and standard deviation
	if len(hourlyCosts) == 0 {
		return nil, nil
	}

	var sum float64
	for _, cost := range hourlyCosts {
		sum += cost
	}
	avg := sum / float64(len(hourlyCosts))

	var varianceSum float64
	for _, cost := range hourlyCosts {
		diff := cost - avg
		varianceSum += diff * diff
	}
	stdDev := 0.0
	if len(hourlyCosts) > 1 {
		stdDev = varianceSum / float64(len(hourlyCosts)-1)
	}

	// Detect anomalies (costs more than threshold standard deviations above average)
	var anomalies []CostAnomaly
	for i, cost := range hourlyCosts {
		if cost > avg+(threshold*stdDev) {
			anomalies = append(anomalies, CostAnomaly{
				Timestamp:       timestamps[i],
				TenantID:        tenantID,
				Cost:            cost,
				AverageCost:     avg,
				DeviationFactor: (cost - avg) / stdDev,
			})
		}
	}

	return anomalies, nil
}

// CostAnomaly represents an unusual cost spike.
type CostAnomaly struct {
	Timestamp       time.Time
	TenantID        api.TenantID
	Cost            float64
	AverageCost     float64
	DeviationFactor float64
}

// CleanupOldRecords removes cost records older than the retention period.
func (ct *CostTracker) CleanupOldRecords(ctx context.Context, retentionDays int) error {
	cutoffTime := time.Now().AddDate(0, 0, -retentionDays)

	query := `DELETE FROM costs WHERE timestamp < $1`

	result, err := ct.db.ExecContext(ctx, query, cutoffTime)
	if err != nil {
		return fmt.Errorf("failed to cleanup old cost records: %w", err)
	}

	deleted, _ := result.RowsAffected()
	ct.logger.InfoContext(ctx, "cleaned up old cost records",
		"deleted", deleted,
		"cutoff_time", cutoffTime,
	)

	return nil
}
