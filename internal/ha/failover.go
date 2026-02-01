package ha

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// FailoverManager manages automatic failover of services
type FailoverManager struct {
	logger      *slog.Logger
	election    *LeaderElection
	replication *StateReplication
	config      FailoverConfig

	// Health tracking
	mu            sync.RWMutex
	healthChecks  map[string]*HealthCheck
	healthStatus  map[string]HealthStatus
	failoverCount map[string]int

	// Callbacks
	onFailover func(service string) error
}

// FailoverConfig configures failover behavior
type FailoverConfig struct {
	// HealthCheckInterval is how often to check service health
	HealthCheckInterval time.Duration

	// HealthCheckTimeout is the timeout for health checks
	HealthCheckTimeout time.Duration

	// FailureThreshold is the number of consecutive failures before failover
	FailureThreshold int

	// CooldownPeriod is the time to wait after failover before trying again
	CooldownPeriod time.Duration

	// MaxFailovers is the max number of failovers allowed in CooldownPeriod
	MaxFailovers int
}

// DefaultFailoverConfig returns default failover configuration
func DefaultFailoverConfig() FailoverConfig {
	return FailoverConfig{
		HealthCheckInterval: 10 * time.Second,
		HealthCheckTimeout:  5 * time.Second,
		FailureThreshold:    3,
		CooldownPeriod:      5 * time.Minute,
		MaxFailovers:        5,
	}
}

// HealthStatus represents the health status of a service
type HealthStatus struct {
	Service          string
	Healthy          bool
	ConsecutiveFails int
	LastCheck        time.Time
	LastFailover     time.Time
	FailoverCount    int
	Message          string
}

// HealthCheck defines a health check function
type HealthCheck struct {
	Name     string
	Check    func(context.Context) error
	Interval time.Duration
	Timeout  time.Duration
}

// NewFailoverManager creates a new failover manager
func NewFailoverManager(
	logger *slog.Logger,
	election *LeaderElection,
	replication *StateReplication,
	config FailoverConfig,
) *FailoverManager {
	return &FailoverManager{
		logger:        logger,
		election:      election,
		replication:   replication,
		config:        config,
		healthChecks:  make(map[string]*HealthCheck),
		healthStatus:  make(map[string]HealthStatus),
		failoverCount: make(map[string]int),
	}
}

// RegisterHealthCheck registers a health check for a service
func (fm *FailoverManager) RegisterHealthCheck(check *HealthCheck) {
	fm.mu.Lock()
	defer fm.mu.Unlock()

	fm.healthChecks[check.Name] = check
	fm.healthStatus[check.Name] = HealthStatus{
		Service: check.Name,
		Healthy: true,
	}

	fm.logger.Info("registered health check",
		"service", check.Name,
		"interval", check.Interval,
	)
}

// OnFailover sets the callback for when failover occurs
func (fm *FailoverManager) OnFailover(callback func(service string) error) {
	fm.onFailover = callback
}

// Start starts the failover manager
func (fm *FailoverManager) Start(ctx context.Context) error {
	fm.logger.Info("starting failover manager")

	var wg sync.WaitGroup

	// Start health checks for each service
	for name, check := range fm.healthChecks {
		wg.Add(1)
		go func(name string, check *HealthCheck) {
			defer wg.Done()
			fm.runHealthCheck(ctx, name, check)
		}(name, check)
	}

	// Start failover monitoring
	wg.Add(1)
	go func() {
		defer wg.Done()
		fm.monitorFailovers(ctx)
	}()

	// Wait for context cancellation
	<-ctx.Done()

	fm.logger.Info("stopping failover manager")
	wg.Wait()

	return nil
}

// runHealthCheck runs a health check periodically
func (fm *FailoverManager) runHealthCheck(ctx context.Context, name string, check *HealthCheck) {
	interval := check.Interval
	if interval == 0 {
		interval = fm.config.HealthCheckInterval
	}

	timeout := check.Timeout
	if timeout == 0 {
		timeout = fm.config.HealthCheckTimeout
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			fm.performHealthCheck(ctx, name, check, timeout)
		}
	}
}

// performHealthCheck performs a single health check
func (fm *FailoverManager) performHealthCheck(ctx context.Context, name string, check *HealthCheck, timeout time.Duration) {
	checkCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	start := time.Now()
	err := check.Check(checkCtx)
	duration := time.Since(start)

	fm.mu.Lock()
	defer fm.mu.Unlock()

	status := fm.healthStatus[name]
	status.LastCheck = time.Now()

	if err != nil {
		status.ConsecutiveFails++
		status.Message = err.Error()

		fm.logger.Warn("health check failed",
			"service", name,
			"consecutive_fails", status.ConsecutiveFails,
			"duration", duration,
			"error", err,
		)

		// Trigger failover if threshold reached
		if status.ConsecutiveFails >= fm.config.FailureThreshold && status.Healthy {
			status.Healthy = false
			fm.healthStatus[name] = status
			fm.mu.Unlock() // Unlock before calling failover
			fm.triggerFailover(ctx, name)
			fm.mu.Lock() // Re-lock before returning
			return
		}
	} else {
		// Health check passed
		if !status.Healthy {
			fm.logger.Info("service recovered",
				"service", name,
				"was_down_for", time.Since(status.LastFailover),
			)
		}

		status.Healthy = true
		status.ConsecutiveFails = 0
		status.Message = "healthy"

		fm.logger.Debug("health check passed",
			"service", name,
			"duration", duration,
		)
	}

	fm.healthStatus[name] = status
}

// triggerFailover triggers failover for a service
func (fm *FailoverManager) triggerFailover(ctx context.Context, service string) {
	fm.logger.Warn("triggering failover", "service", service)

	fm.mu.Lock()
	status := fm.healthStatus[service]

	// Check if we've exceeded max failovers in cooldown period
	if time.Since(status.LastFailover) < fm.config.CooldownPeriod {
		if status.FailoverCount >= fm.config.MaxFailovers {
			fm.mu.Unlock()
			fm.logger.Error("max failovers exceeded, not triggering failover",
				"service", service,
				"count", status.FailoverCount,
				"cooldown", fm.config.CooldownPeriod,
			)
			return
		}
	} else {
		// Reset failover count after cooldown period
		status.FailoverCount = 0
	}

	status.LastFailover = time.Now()
	status.FailoverCount++
	fm.healthStatus[service] = status
	fm.mu.Unlock()

	// Execute failover callback
	if fm.onFailover != nil {
		if err := fm.onFailover(service); err != nil {
			fm.logger.Error("failover callback failed",
				"service", service,
				"error", err,
			)
			return
		}
	}

	// Replicate failover event
	if fm.replication != nil {
		failoverEvent := map[string]interface{}{
			"service":   service,
			"timestamp": time.Now(),
			"count":     status.FailoverCount,
		}
		if err := fm.replication.Put(ctx, "failover/"+service, failoverEvent); err != nil {
			fm.logger.Error("failed to replicate failover event", "error", err)
		}
	}

	fm.logger.Info("failover triggered",
		"service", service,
		"count", status.FailoverCount,
	)
}

// monitorFailovers monitors for failover events from other nodes
func (fm *FailoverManager) monitorFailovers(ctx context.Context) {
	if fm.replication == nil {
		return
	}

	eventCh := fm.replication.Watch(ctx, "failover/")

	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-eventCh:
			if !ok {
				return
			}

			fm.logger.Info("received failover event from cluster",
				"key", event.Key,
				"timestamp", event.Timestamp,
			)
		}
	}
}

// GetHealthStatus returns the health status of a service
func (fm *FailoverManager) GetHealthStatus(service string) (HealthStatus, bool) {
	fm.mu.RLock()
	defer fm.mu.RUnlock()

	status, ok := fm.healthStatus[service]
	return status, ok
}

// GetAllHealthStatus returns the health status of all services
func (fm *FailoverManager) GetAllHealthStatus() map[string]HealthStatus {
	fm.mu.RLock()
	defer fm.mu.RUnlock()

	status := make(map[string]HealthStatus)
	for k, v := range fm.healthStatus {
		status[k] = v
	}

	return status
}

// IsHealthy returns true if a service is healthy
func (fm *FailoverManager) IsHealthy(service string) bool {
	fm.mu.RLock()
	defer fm.mu.RUnlock()

	status, ok := fm.healthStatus[service]
	if !ok {
		return false
	}

	return status.Healthy
}

// ForceFailover manually triggers failover for a service
func (fm *FailoverManager) ForceFailover(ctx context.Context, service string) error {
	fm.mu.RLock()
	_, exists := fm.healthChecks[service]
	fm.mu.RUnlock()

	if !exists {
		return fmt.Errorf("service not found: %s", service)
	}

	fm.logger.Info("forcing failover", "service", service)
	fm.triggerFailover(ctx, service)

	return nil
}

// HACluster represents a high availability cluster
type HACluster struct {
	logger      *slog.Logger
	election    *LeaderElection
	replication *StateReplication
	failover    *FailoverManager
	config      ClusterConfig
}

// ClusterConfig configures the HA cluster
type ClusterConfig struct {
	NodeName          string
	ElectionConfig    ElectionConfig
	ReplicationConfig ReplicationConfig
	FailoverConfig    FailoverConfig
}

// DefaultClusterConfig returns default cluster configuration
func DefaultClusterConfig(nodeName string) ClusterConfig {
	electionConfig := DefaultElectionConfig()
	electionConfig.LeaderName = nodeName

	return ClusterConfig{
		NodeName:          nodeName,
		ElectionConfig:    electionConfig,
		ReplicationConfig: DefaultReplicationConfig(),
		FailoverConfig:    DefaultFailoverConfig(),
	}
}

// NewHACluster creates a new HA cluster
func NewHACluster(logger *slog.Logger, config ClusterConfig) (*HACluster, error) {
	// Create leader election
	election, err := NewLeaderElection(logger, config.ElectionConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create leader election: %w", err)
	}

	// Create state replication
	replication, err := NewStateReplication(logger, config.ReplicationConfig)
	if err != nil {
		election.Close()
		return nil, fmt.Errorf("failed to create state replication: %w", err)
	}

	// Create failover manager
	failover := NewFailoverManager(logger, election, replication, config.FailoverConfig)

	return &HACluster{
		logger:      logger,
		election:    election,
		replication: replication,
		failover:    failover,
		config:      config,
	}, nil
}

// Start starts the HA cluster
func (hac *HACluster) Start(ctx context.Context) error {
	hac.logger.Info("starting HA cluster", "node", hac.config.NodeName)

	// Start state replication auto-sync
	go hac.replication.StartAutoSync(ctx)

	// Start failover manager
	go func() {
		if err := hac.failover.Start(ctx); err != nil {
			hac.logger.Error("failover manager error", "error", err)
		}
	}()

	// Run leadership manager
	leadershipMgr := &LeadershipManager{
		logger:   hac.logger,
		election: hac.election,
		config:   hac.config.ElectionConfig,
	}

	return leadershipMgr.Run(ctx)
}

// IsLeader returns true if this node is the leader
func (hac *HACluster) IsLeader(ctx context.Context) (bool, error) {
	return hac.election.IsLeader(ctx)
}

// GetLeader returns the current leader
func (hac *HACluster) GetLeader(ctx context.Context) (string, error) {
	return hac.election.GetLeader(ctx)
}

// GetState gets replicated state
func (hac *HACluster) GetState(ctx context.Context, key string) (interface{}, error) {
	return hac.replication.Get(ctx, key)
}

// PutState puts replicated state
func (hac *HACluster) PutState(ctx context.Context, key string, value interface{}) error {
	return hac.replication.Put(ctx, key, value)
}

// RegisterHealthCheck registers a health check
func (hac *HACluster) RegisterHealthCheck(check *HealthCheck) {
	hac.failover.RegisterHealthCheck(check)
}

// GetHealthStatus gets health status for a service
func (hac *HACluster) GetHealthStatus(service string) (HealthStatus, bool) {
	return hac.failover.GetHealthStatus(service)
}

// Close closes the HA cluster
func (hac *HACluster) Close() error {
	hac.logger.Info("closing HA cluster")

	if err := hac.election.Close(); err != nil {
		hac.logger.Error("failed to close election", "error", err)
	}

	if err := hac.replication.Close(); err != nil {
		hac.logger.Error("failed to close replication", "error", err)
	}

	return nil
}
