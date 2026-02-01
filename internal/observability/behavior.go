package observability

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/dnakitare/aether/pkg/api"
)

// BehaviorMonitor tracks agent behavior patterns and detects anomalies.
type BehaviorMonitor struct {
	logger *slog.Logger
	mu     sync.RWMutex

	// Baseline behavior patterns per tenant
	baselines map[api.TenantID]*BehaviorBaseline

	// Recent behavior windows
	windows map[api.TenantID]*BehaviorWindow

	// Alert callback
	alertCallback AlertCallback

	config MonitorConfig
}

// MonitorConfig holds configuration for behavior monitoring.
type MonitorConfig struct {
	// WindowSize is the time window for behavior tracking
	WindowSize time.Duration

	// BaselineSize is how many windows to use for baseline calculation
	BaselineSize int

	// AnomalyThreshold is the standard deviation multiplier for anomaly detection
	AnomalyThreshold float64

	// AlertCooldown prevents alert spam
	AlertCooldown time.Duration
}

// DefaultMonitorConfig returns default monitoring configuration.
func DefaultMonitorConfig() MonitorConfig {
	return MonitorConfig{
		WindowSize:       1 * time.Hour,
		BaselineSize:     24,  // 24 hours of hourly windows
		AnomalyThreshold: 3.0, // 3 standard deviations
		AlertCooldown:    15 * time.Minute,
	}
}

// BehaviorBaseline represents normal behavior patterns.
type BehaviorBaseline struct {
	TenantID api.TenantID

	// Average metrics
	AvgAgentSpawns float64
	AvgAPICalls    float64
	AvgCPUUsage    float64
	AvgMemoryUsage float64

	// Standard deviations
	StdDevAgentSpawns float64
	StdDevAPICalls    float64
	StdDevCPUUsage    float64
	StdDevMemoryUsage float64

	// Sample count
	SampleCount int

	LastUpdated time.Time
}

// BehaviorWindow tracks behavior in a time window.
type BehaviorWindow struct {
	TenantID  api.TenantID
	StartTime time.Time
	EndTime   time.Time

	AgentSpawns int
	APICalls    int
	CPUUsage    float64
	MemoryUsage float64

	// Agent-specific patterns
	AgentPatterns map[api.AgentID]*AgentBehavior
}

// AgentBehavior tracks individual agent behavior.
type AgentBehavior struct {
	AgentID     api.AgentID
	APICalls    int
	CPUUsage    float64
	MemoryUsage float64
	Spawns      int // How many child agents spawned
	LastSeen    time.Time
}

// Anomaly represents detected abnormal behavior.
type Anomaly struct {
	Timestamp     time.Time
	TenantID      api.TenantID
	AgentID       api.AgentID
	Type          AnomalyType
	Severity      Severity
	Description   string
	CurrentValue  float64
	BaselineValue float64
	Deviation     float64
	Action        ResponseAction
}

// AnomalyType represents the type of anomaly.
type AnomalyType string

const (
	AnomalySpawning      AnomalyType = "excessive_spawning"
	AnomalyAPICalls      AnomalyType = "excessive_api_calls"
	AnomalyCPUUsage      AnomalyType = "excessive_cpu"
	AnomalyMemoryUsage   AnomalyType = "excessive_memory"
	AnomalyRapidRequests AnomalyType = "rapid_requests"
)

// Severity levels for anomalies.
type Severity string

const (
	SeverityLow      Severity = "low"
	SeverityMedium   Severity = "medium"
	SeverityHigh     Severity = "high"
	SeverityCritical Severity = "critical"
)

// ResponseAction defines automated response to anomalies.
type ResponseAction string

const (
	ActionNone      ResponseAction = "none"
	ActionThrottle  ResponseAction = "throttle"
	ActionRevoke    ResponseAction = "revoke_permissions"
	ActionTerminate ResponseAction = "terminate"
	ActionEscalate  ResponseAction = "escalate"
)

// AlertCallback is called when an anomaly is detected.
type AlertCallback func(ctx context.Context, anomaly *Anomaly) error

// NewBehaviorMonitor creates a new behavior monitor.
func NewBehaviorMonitor(logger *slog.Logger, config MonitorConfig, alertCallback AlertCallback) *BehaviorMonitor {
	return &BehaviorMonitor{
		logger:        logger.With("component", "behavior_monitor"),
		baselines:     make(map[api.TenantID]*BehaviorBaseline),
		windows:       make(map[api.TenantID]*BehaviorWindow),
		alertCallback: alertCallback,
		config:        config,
	}
}

// RecordAgentSpawn records an agent spawn event.
func (bm *BehaviorMonitor) RecordAgentSpawn(ctx context.Context, tenantID api.TenantID, agentID api.AgentID, parentID api.AgentID) {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	window := bm.getOrCreateWindow(tenantID)
	window.AgentSpawns++

	if parentID != "" {
		if pattern, exists := window.AgentPatterns[parentID]; exists {
			pattern.Spawns++
		}
	}

	// Initialize new agent pattern
	if _, exists := window.AgentPatterns[agentID]; !exists {
		window.AgentPatterns[agentID] = &AgentBehavior{
			AgentID:  agentID,
			LastSeen: time.Now(),
		}
	}

	bm.logger.DebugContext(ctx, "recorded agent spawn",
		"tenant_id", tenantID,
		"agent_id", agentID,
		"parent_id", parentID,
	)
}

// RecordAPICall records an API call from an agent.
func (bm *BehaviorMonitor) RecordAPICall(ctx context.Context, tenantID api.TenantID, agentID api.AgentID) {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	window := bm.getOrCreateWindow(tenantID)
	window.APICalls++

	if pattern, exists := window.AgentPatterns[agentID]; exists {
		pattern.APICalls++
		pattern.LastSeen = time.Now()
	} else {
		window.AgentPatterns[agentID] = &AgentBehavior{
			AgentID:  agentID,
			APICalls: 1,
			LastSeen: time.Now(),
		}
	}
}

// RecordResourceUsage records CPU and memory usage.
func (bm *BehaviorMonitor) RecordResourceUsage(ctx context.Context, tenantID api.TenantID, agentID api.AgentID, cpuCores, memoryGB float64) {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	window := bm.getOrCreateWindow(tenantID)
	window.CPUUsage += cpuCores
	window.MemoryUsage += memoryGB

	if pattern, exists := window.AgentPatterns[agentID]; exists {
		pattern.CPUUsage = cpuCores
		pattern.MemoryUsage = memoryGB
		pattern.LastSeen = time.Now()
	} else {
		window.AgentPatterns[agentID] = &AgentBehavior{
			AgentID:     agentID,
			CPUUsage:    cpuCores,
			MemoryUsage: memoryGB,
			LastSeen:    time.Now(),
		}
	}
}

// CheckAnomalies checks for behavioral anomalies.
func (bm *BehaviorMonitor) CheckAnomalies(ctx context.Context, tenantID api.TenantID) ([]*Anomaly, error) {
	bm.mu.RLock()
	defer bm.mu.RUnlock()

	window := bm.windows[tenantID]
	if window == nil {
		return nil, nil
	}

	baseline := bm.baselines[tenantID]
	if baseline == nil || baseline.SampleCount < 3 {
		// Not enough data for baseline yet
		return nil, nil
	}

	var anomalies []*Anomaly

	// Check agent spawning
	if float64(window.AgentSpawns) > baseline.AvgAgentSpawns+(bm.config.AnomalyThreshold*baseline.StdDevAgentSpawns) {
		deviation := (float64(window.AgentSpawns) - baseline.AvgAgentSpawns) / baseline.StdDevAgentSpawns
		anomalies = append(anomalies, &Anomaly{
			Timestamp:     time.Now(),
			TenantID:      tenantID,
			Type:          AnomalySpawning,
			Severity:      bm.calculateSeverity(deviation),
			Description:   fmt.Sprintf("Excessive agent spawning detected: %d spawns (baseline: %.1f)", window.AgentSpawns, baseline.AvgAgentSpawns),
			CurrentValue:  float64(window.AgentSpawns),
			BaselineValue: baseline.AvgAgentSpawns,
			Deviation:     deviation,
			Action:        bm.determineAction(AnomalySpawning, deviation),
		})
	}

	// Check API calls
	if float64(window.APICalls) > baseline.AvgAPICalls+(bm.config.AnomalyThreshold*baseline.StdDevAPICalls) {
		deviation := (float64(window.APICalls) - baseline.AvgAPICalls) / baseline.StdDevAPICalls
		anomalies = append(anomalies, &Anomaly{
			Timestamp:     time.Now(),
			TenantID:      tenantID,
			Type:          AnomalyAPICalls,
			Severity:      bm.calculateSeverity(deviation),
			Description:   fmt.Sprintf("Excessive API calls detected: %d calls (baseline: %.1f)", window.APICalls, baseline.AvgAPICalls),
			CurrentValue:  float64(window.APICalls),
			BaselineValue: baseline.AvgAPICalls,
			Deviation:     deviation,
			Action:        bm.determineAction(AnomalyAPICalls, deviation),
		})
	}

	// Check CPU usage
	if window.CPUUsage > baseline.AvgCPUUsage+(bm.config.AnomalyThreshold*baseline.StdDevCPUUsage) {
		deviation := (window.CPUUsage - baseline.AvgCPUUsage) / baseline.StdDevCPUUsage
		anomalies = append(anomalies, &Anomaly{
			Timestamp:     time.Now(),
			TenantID:      tenantID,
			Type:          AnomalyCPUUsage,
			Severity:      bm.calculateSeverity(deviation),
			Description:   fmt.Sprintf("Excessive CPU usage detected: %.2f cores (baseline: %.2f)", window.CPUUsage, baseline.AvgCPUUsage),
			CurrentValue:  window.CPUUsage,
			BaselineValue: baseline.AvgCPUUsage,
			Deviation:     deviation,
			Action:        bm.determineAction(AnomalyCPUUsage, deviation),
		})
	}

	return anomalies, nil
}

// UpdateBaseline updates the baseline for a tenant.
func (bm *BehaviorMonitor) UpdateBaseline(ctx context.Context, tenantID api.TenantID) {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	// This would typically aggregate historical windows
	// For now, we'll create a simple implementation

	window := bm.windows[tenantID]
	if window == nil {
		return
	}

	baseline := bm.baselines[tenantID]
	if baseline == nil {
		baseline = &BehaviorBaseline{
			TenantID: tenantID,
		}
		bm.baselines[tenantID] = baseline
	}

	// Simple exponential moving average update
	alpha := 0.3 // Weight for new data
	baseline.AvgAgentSpawns = (1-alpha)*baseline.AvgAgentSpawns + alpha*float64(window.AgentSpawns)
	baseline.AvgAPICalls = (1-alpha)*baseline.AvgAPICalls + alpha*float64(window.APICalls)
	baseline.AvgCPUUsage = (1-alpha)*baseline.AvgCPUUsage + alpha*window.CPUUsage
	baseline.AvgMemoryUsage = (1-alpha)*baseline.AvgMemoryUsage + alpha*window.MemoryUsage

	baseline.SampleCount++
	baseline.LastUpdated = time.Now()

	bm.logger.DebugContext(ctx, "updated baseline",
		"tenant_id", tenantID,
		"sample_count", baseline.SampleCount,
	)
}

func (bm *BehaviorMonitor) getOrCreateWindow(tenantID api.TenantID) *BehaviorWindow {
	window := bm.windows[tenantID]
	if window == nil || time.Since(window.StartTime) > bm.config.WindowSize {
		// Create new window
		window = &BehaviorWindow{
			TenantID:      tenantID,
			StartTime:     time.Now(),
			EndTime:       time.Now().Add(bm.config.WindowSize),
			AgentPatterns: make(map[api.AgentID]*AgentBehavior),
		}
		bm.windows[tenantID] = window
	}
	return window
}

func (bm *BehaviorMonitor) calculateSeverity(deviation float64) Severity {
	switch {
	case deviation >= 5.0:
		return SeverityCritical
	case deviation >= 4.0:
		return SeverityHigh
	case deviation >= 3.0:
		return SeverityMedium
	default:
		return SeverityLow
	}
}

func (bm *BehaviorMonitor) determineAction(anomalyType AnomalyType, deviation float64) ResponseAction {
	// Critical deviations get immediate action
	if deviation >= 5.0 {
		switch anomalyType {
		case AnomalySpawning:
			return ActionRevoke
		case AnomalyAPICalls:
			return ActionThrottle
		case AnomalyCPUUsage, AnomalyMemoryUsage:
			return ActionTerminate
		}
	}

	// High deviations get throttling
	if deviation >= 4.0 {
		return ActionThrottle
	}

	// Medium deviations get escalation
	if deviation >= 3.0 {
		return ActionEscalate
	}

	return ActionNone
}

// StartMonitoring starts the background monitoring loop.
func (bm *BehaviorMonitor) StartMonitoring(ctx context.Context) {
	ticker := time.NewTicker(bm.config.WindowSize)
	defer ticker.Stop()

	bm.logger.Info("starting behavior monitoring",
		"window_size", bm.config.WindowSize,
		"threshold", bm.config.AnomalyThreshold,
	)

	for {
		select {
		case <-ctx.Done():
			bm.logger.Info("stopping behavior monitoring")
			return
		case <-ticker.C:
			bm.checkAllTenants(ctx)
		}
	}
}

func (bm *BehaviorMonitor) checkAllTenants(ctx context.Context) {
	bm.mu.RLock()
	tenantIDs := make([]api.TenantID, 0, len(bm.windows))
	for tenantID := range bm.windows {
		tenantIDs = append(tenantIDs, tenantID)
	}
	bm.mu.RUnlock()

	for _, tenantID := range tenantIDs {
		// Check for anomalies
		anomalies, err := bm.CheckAnomalies(ctx, tenantID)
		if err != nil {
			bm.logger.ErrorContext(ctx, "failed to check anomalies",
				"tenant_id", tenantID,
				"error", err,
			)
			continue
		}

		// Trigger alerts
		for _, anomaly := range anomalies {
			if bm.alertCallback != nil {
				if err := bm.alertCallback(ctx, anomaly); err != nil {
					bm.logger.ErrorContext(ctx, "alert callback failed",
						"anomaly_type", anomaly.Type,
						"tenant_id", tenantID,
						"error", err,
					)
				}
			}
		}

		// Update baseline
		bm.UpdateBaseline(ctx, tenantID)
	}
}
