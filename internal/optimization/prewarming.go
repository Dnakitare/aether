package optimization

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// VMFactory creates a new VM for a given workload type.
// The runtime injects its VM manager via this interface so the pool can
// create real Firecracker instances instead of stubs.
type VMFactory interface {
	CreatePrewarmedVM(ctx context.Context, workloadType WorkloadType) (interface{}, error)
}

// PrewarmingPool manages a pool of pre-warmed VMs for fast startup
type PrewarmingPool struct {
	logger  *slog.Logger
	config  PrewarmingConfig
	factory VMFactory // optional; nil falls back to stub VMs

	// Pool management
	mu          sync.RWMutex
	pools       map[WorkloadType]*vmPool
	poolMetrics map[WorkloadType]*PoolMetrics
}

// PrewarmingConfig configures VM pre-warming
type PrewarmingConfig struct {
	// Enabled enables VM pre-warming
	Enabled bool

	// PoolSize is the number of pre-warmed VMs per workload type
	PoolSize map[WorkloadType]int

	// RefillThreshold is when to start creating new VMs (% of pool size)
	RefillThreshold float64

	// MaxIdleTime is how long to keep idle VMs before terminating
	MaxIdleTime time.Duration

	// WarmupTime is how long to warm up a VM
	WarmupTime time.Duration

	// HealthCheckInterval is how often to check VM health
	HealthCheckInterval time.Duration
}

// DefaultPrewarmingConfig returns default pre-warming configuration
func DefaultPrewarmingConfig() PrewarmingConfig {
	return PrewarmingConfig{
		Enabled: false,
		PoolSize: map[WorkloadType]int{
			WorkloadCodeExecution:  5,
			WorkloadLLMAgent:       3,
			WorkloadDataProcessing: 2,
			WorkloadLongRunning:    1,
		},
		RefillThreshold:     0.3, // Refill when 30% remaining
		MaxIdleTime:         15 * time.Minute,
		WarmupTime:          30 * time.Second,
		HealthCheckInterval: 30 * time.Second,
	}
}

// WorkloadType represents different workload profiles
type WorkloadType string

const (
	// WorkloadCodeExecution is for code execution (Python, Node.js)
	WorkloadCodeExecution WorkloadType = "code-execution"

	// WorkloadLLMAgent is for LLM agents with API calls
	WorkloadLLMAgent WorkloadType = "llm-agent"

	// WorkloadDataProcessing is for data processing workloads
	WorkloadDataProcessing WorkloadType = "data-processing"

	// WorkloadLongRunning is for long-running agents with checkpointing
	WorkloadLongRunning WorkloadType = "long-running"
)

// vmPool manages a pool of VMs for a specific workload type
type vmPool struct {
	workloadType WorkloadType
	available    []*PrewarmedVM
	inUse        map[string]*PrewarmedVM
	mu           sync.RWMutex
}

// PrewarmedVM represents a pre-warmed VM
type PrewarmedVM struct {
	ID           string
	WorkloadType WorkloadType
	CreatedAt    time.Time
	LastUsed     time.Time
	Healthy      bool
	InUse        bool
	// VM holds the underlying VM object created by the factory (nil for stub VMs).
	VM interface{}
}

// PoolMetrics tracks metrics for a VM pool
type PoolMetrics struct {
	TotalCreated  int64
	TotalAcquired int64
	TotalReleased int64
	TotalExpired  int64
	CurrentSize   int
	AvailableSize int
	InUseSize     int
	HitRate       float64
	AvgWaitTime   time.Duration
}

// SetFactory injects a VMFactory so the pool creates real VMs instead of stubs.
// Must be called before Start.
func (pp *PrewarmingPool) SetFactory(f VMFactory) {
	pp.factory = f
}

// NewPrewarmingPool creates a new pre-warming pool
func NewPrewarmingPool(logger *slog.Logger, config PrewarmingConfig) *PrewarmingPool {
	pool := &PrewarmingPool{
		logger:      logger,
		config:      config,
		pools:       make(map[WorkloadType]*vmPool),
		poolMetrics: make(map[WorkloadType]*PoolMetrics),
	}

	// Initialize pools for each workload type
	for workloadType, size := range config.PoolSize {
		pool.pools[workloadType] = &vmPool{
			workloadType: workloadType,
			available:    make([]*PrewarmedVM, 0, size),
			inUse:        make(map[string]*PrewarmedVM),
		}
		pool.poolMetrics[workloadType] = &PoolMetrics{}
	}

	return pool
}

// Start starts the pre-warming pool
func (pp *PrewarmingPool) Start(ctx context.Context) error {
	if pp.factory == nil && pp.config.Enabled {
		pp.logger.Warn("prewarming pool enabled but no VMFactory set — disabling to prevent stub VM creation",
			"action", "pool_disabled")
		pp.config.Enabled = false
	}

	if !pp.config.Enabled {
		pp.logger.Info("pre-warming disabled, skipping pool startup")
		return nil
	}

	pp.logger.Info("starting pre-warming pool")

	// Fill initial pools
	for workloadType, size := range pp.config.PoolSize {
		pp.logger.Info("filling initial pool",
			"workload_type", workloadType,
			"size", size,
		)

		for i := 0; i < size; i++ {
			if err := pp.createVM(ctx, workloadType); err != nil {
				pp.logger.Error("failed to create initial VM",
					"workload_type", workloadType,
					"error", err,
				)
			}
		}
	}

	// Start background tasks
	go pp.maintainPools(ctx)
	go pp.cleanupExpiredVMs(ctx)
	go pp.healthCheckVMs(ctx)

	return nil
}

// AcquireVM acquires a pre-warmed VM from the pool
func (pp *PrewarmingPool) AcquireVM(ctx context.Context, workloadType WorkloadType) (*PrewarmedVM, error) {
	start := time.Now()

	pool := pp.getPool(workloadType)
	if pool == nil {
		return nil, fmt.Errorf("no pool for workload type: %s", workloadType)
	}

	pool.mu.Lock()

	// Try to get a VM from available pool
	if len(pool.available) > 0 {
		vm := pool.available[0]
		pool.available = pool.available[1:]
		pool.inUse[vm.ID] = vm

		vm.InUse = true
		vm.LastUsed = time.Now()

		// Capture metrics data while holding lock
		inUseSize := len(pool.inUse)
		availableSize := len(pool.available)
		refillThreshold := int(float64(pp.config.PoolSize[workloadType]) * pp.config.RefillThreshold)
		needsRefill := availableSize < refillThreshold

		pool.mu.Unlock()

		// Update metrics after releasing lock to avoid deadlock
		pp.updateMetrics(workloadType, func(m *PoolMetrics) {
			m.TotalAcquired++
			m.InUseSize = inUseSize
			m.AvailableSize = availableSize
			if m.TotalAcquired+m.TotalCreated > 0 {
				m.HitRate = float64(m.TotalAcquired) / float64(m.TotalAcquired+m.TotalCreated)
			}
			m.AvgWaitTime = time.Since(start)
		})

		pp.logger.Info("acquired pre-warmed VM",
			"vm_id", vm.ID,
			"workload_type", workloadType,
			"wait_time", time.Since(start),
		)

		// Trigger refill if below threshold
		if needsRefill {
			go pp.refillPool(ctx, workloadType)
		}

		return vm, nil
	}

	// No pre-warmed VMs available, create new one
	pp.logger.Warn("no pre-warmed VMs available, creating new VM",
		"workload_type", workloadType,
	)

	// Release pool lock before creating new VM (which may take time)
	pool.mu.Unlock()

	vm, err := pp.createVMSync(ctx, workloadType)
	if err != nil {
		return nil, err
	}

	// Re-acquire lock to update pool
	pool.mu.Lock()
	pool.inUse[vm.ID] = vm
	vm.InUse = true
	inUseSize := len(pool.inUse)
	pool.mu.Unlock()

	// Update metrics after releasing lock
	pp.updateMetrics(workloadType, func(m *PoolMetrics) {
		m.TotalCreated++
		m.InUseSize = inUseSize
		m.AvgWaitTime = time.Since(start)
	})

	return vm, nil
}

// ReleaseVM returns a VM to the pool
func (pp *PrewarmingPool) ReleaseVM(ctx context.Context, vmID string) error {
	pp.mu.RLock()
	// Find which pool this VM belongs to
	for workloadType, pool := range pp.pools {
		pool.mu.Lock()
		vm, exists := pool.inUse[vmID]
		if exists {
			delete(pool.inUse, vmID)

			vm.InUse = false
			vm.LastUsed = time.Now()

			// Return to available pool if healthy
			if vm.Healthy {
				pool.available = append(pool.available, vm)
			}

			// Capture metrics data while holding locks
			inUseSize := len(pool.inUse)
			availableSize := len(pool.available)

			pool.mu.Unlock()
			pp.mu.RUnlock()

			// Update metrics after releasing locks to avoid deadlock
			pp.updateMetrics(workloadType, func(m *PoolMetrics) {
				m.TotalReleased++
				m.InUseSize = inUseSize
				m.AvailableSize = availableSize
			})

			pp.logger.Info("released VM to pool",
				"vm_id", vmID,
				"workload_type", workloadType,
			)

			return nil
		}
		pool.mu.Unlock()
	}
	pp.mu.RUnlock()

	return fmt.Errorf("VM not found: %s", vmID)
}

// createVM creates a new pre-warmed VM asynchronously
func (pp *PrewarmingPool) createVM(ctx context.Context, workloadType WorkloadType) error {
	go func() {
		vm, err := pp.createVMSync(ctx, workloadType)
		if err != nil {
			pp.logger.Error("failed to create VM", "error", err)
			return
		}

		pool := pp.getPool(workloadType)

		// Capture metrics values while holding pool.mu, then release before
		// calling updateMetrics (which acquires pp.mu). This preserves the
		// consistent lock order: pool.mu is never held while acquiring pp.mu.
		pool.mu.Lock()
		pool.available = append(pool.available, vm)
		availableSize := len(pool.available)
		pool.mu.Unlock()

		pp.updateMetrics(workloadType, func(m *PoolMetrics) {
			m.TotalCreated++
			m.CurrentSize++
			m.AvailableSize = availableSize
		})
	}()

	return nil
}

// createVMSync creates a new pre-warmed VM synchronously.
// When a VMFactory has been injected it creates a real VM; otherwise it falls
// back to a lightweight stub so the pool can still track warm-up slots.
func (pp *PrewarmingPool) createVMSync(ctx context.Context, workloadType WorkloadType) (*PrewarmedVM, error) {
	start := time.Now()
	vmID := fmt.Sprintf("prewarmed-%s-%d", workloadType, time.Now().UnixNano())

	pp.logger.Info("creating pre-warmed VM",
		"vm_id", vmID,
		"workload_type", workloadType,
	)

	var vmObj interface{}
	if pp.factory != nil {
		var err error
		vmObj, err = pp.factory.CreatePrewarmedVM(ctx, workloadType)
		if err != nil {
			return nil, fmt.Errorf("factory failed to create VM for %s: %w", workloadType, err)
		}
	} else {
		// Stub: simulate warmup latency only (no real VM).
		time.Sleep(pp.config.WarmupTime)
	}

	pvm := &PrewarmedVM{
		ID:           vmID,
		WorkloadType: workloadType,
		CreatedAt:    time.Now(),
		LastUsed:     time.Now(),
		Healthy:      true,
		InUse:        false,
		VM:           vmObj,
	}

	pp.logger.Info("created pre-warmed VM",
		"vm_id", vmID,
		"workload_type", workloadType,
		"real_vm", vmObj != nil,
		"duration", time.Since(start),
	)

	return pvm, nil
}

// maintainPools maintains pool sizes
func (pp *PrewarmingPool) maintainPools(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			pp.checkAndRefillPools(ctx)
		}
	}
}

// checkAndRefillPools checks pool sizes and refills if needed.
//
// It snapshots the workload types under a brief read lock and releases pp.mu
// before calling refillPool. Holding pp.mu across refillPool would re-acquire
// the read lock via getPool; if a writer (e.g. updateMetrics) arrived in
// between, Go's RWMutex blocks the nested reader behind the waiting writer,
// deadlocking the maintenance loop.
func (pp *PrewarmingPool) checkAndRefillPools(ctx context.Context) {
	type poolCheck struct {
		workloadType WorkloadType
		pool         *vmPool
	}

	pp.mu.RLock()
	checks := make([]poolCheck, 0, len(pp.pools))
	for workloadType, pool := range pp.pools {
		checks = append(checks, poolCheck{workloadType, pool})
	}
	config := pp.config
	pp.mu.RUnlock()

	for _, c := range checks {
		c.pool.mu.RLock()
		available := len(c.pool.available)
		c.pool.mu.RUnlock()

		targetSize := config.PoolSize[c.workloadType]
		refillThreshold := int(float64(targetSize) * config.RefillThreshold)

		if available < refillThreshold {
			pp.logger.Info("pool below threshold, refilling",
				"workload_type", c.workloadType,
				"available", available,
				"target", targetSize,
			)

			pp.refillPool(ctx, c.workloadType)
		}
	}
}

// refillPool refills a pool to target size
func (pp *PrewarmingPool) refillPool(ctx context.Context, workloadType WorkloadType) {
	pool := pp.getPool(workloadType)
	if pool == nil {
		return
	}

	pool.mu.RLock()
	available := len(pool.available)
	targetSize := pp.config.PoolSize[workloadType]
	pool.mu.RUnlock()

	needed := targetSize - available

	for i := 0; i < needed; i++ {
		if err := pp.createVM(ctx, workloadType); err != nil {
			pp.logger.Error("failed to create VM during refill", "error", err)
		}
	}
}

// cleanupExpiredVMs removes expired VMs from pools
func (pp *PrewarmingPool) cleanupExpiredVMs(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			pp.removeExpiredVMs()
		}
	}
}

// removeExpiredVMs removes VMs that have been idle too long
func (pp *PrewarmingPool) removeExpiredVMs() {
	pp.mu.RLock()

	// Collect updates for each workload type
	type metricsUpdate struct {
		workloadType  WorkloadType
		expired       int
		availableSize int
	}
	var updates []metricsUpdate

	for workloadType, pool := range pp.pools {
		pool.mu.Lock()

		var kept []*PrewarmedVM
		expired := 0

		for _, vm := range pool.available {
			if time.Since(vm.LastUsed) > pp.config.MaxIdleTime {
				pp.logger.Info("removing expired VM",
					"vm_id", vm.ID,
					"workload_type", workloadType,
					"idle_time", time.Since(vm.LastUsed),
				)
				expired++
			} else {
				kept = append(kept, vm)
			}
		}

		pool.available = kept

		if expired > 0 {
			updates = append(updates, metricsUpdate{
				workloadType:  workloadType,
				expired:       expired,
				availableSize: len(pool.available),
			})
		}

		pool.mu.Unlock()
	}

	pp.mu.RUnlock()

	// Update metrics after releasing locks to avoid deadlock
	for _, update := range updates {
		pp.updateMetrics(update.workloadType, func(m *PoolMetrics) {
			m.TotalExpired += int64(update.expired)
			m.CurrentSize -= update.expired
			m.AvailableSize = update.availableSize
		})
	}
}

// healthCheckVMs performs health checks on all VMs
func (pp *PrewarmingPool) healthCheckVMs(ctx context.Context) {
	ticker := time.NewTicker(pp.config.HealthCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			pp.checkVMHealth()
		}
	}
}

// checkVMHealth checks health of all VMs in pools
func (pp *PrewarmingPool) checkVMHealth() {
	pp.mu.RLock()
	defer pp.mu.RUnlock()

	for _, pool := range pp.pools {
		pool.mu.Lock()

		var healthy []*PrewarmedVM
		unhealthy := 0

		for _, vm := range pool.available {
			// In production: actual health check
			if vm.Healthy {
				healthy = append(healthy, vm)
			} else {
				unhealthy++
			}
		}

		pool.available = healthy
		pool.mu.Unlock()

		if unhealthy > 0 {
			pp.logger.Warn("removed unhealthy VMs",
				"workload_type", pool.workloadType,
				"count", unhealthy,
			)
		}
	}
}

// DiscardVM removes a VM from the in-use set without returning it to the
// available pool. Use this when the underlying VM has been destroyed and
// cannot be reused (e.g. after agent destroy). The pool's maintenance loop
// will create a replacement to keep the pool at target size.
func (pp *PrewarmingPool) DiscardVM(vmID string) {
	pp.mu.RLock()
	defer pp.mu.RUnlock()

	for workloadType, pool := range pp.pools {
		pool.mu.Lock()
		if _, exists := pool.inUse[vmID]; exists {
			delete(pool.inUse, vmID)
			inUseSize := len(pool.inUse)
			pool.mu.Unlock()

			pp.updateMetrics(workloadType, func(m *PoolMetrics) {
				m.InUseSize = inUseSize
				if m.CurrentSize > 0 {
					m.CurrentSize--
				}
			})
			return
		}
		pool.mu.Unlock()
	}
}

// getPool gets the pool for a workload type
func (pp *PrewarmingPool) getPool(workloadType WorkloadType) *vmPool {
	pp.mu.RLock()
	defer pp.mu.RUnlock()
	return pp.pools[workloadType]
}

// updateMetrics updates metrics for a workload type
func (pp *PrewarmingPool) updateMetrics(workloadType WorkloadType, updateFn func(*PoolMetrics)) {
	pp.mu.Lock()
	defer pp.mu.Unlock()

	metrics := pp.poolMetrics[workloadType]
	updateFn(metrics)
}

// GetMetrics returns metrics for all pools
func (pp *PrewarmingPool) GetMetrics() map[WorkloadType]*PoolMetrics {
	pp.mu.RLock()
	defer pp.mu.RUnlock()

	metrics := make(map[WorkloadType]*PoolMetrics)
	for workloadType, m := range pp.poolMetrics {
		// Copy metrics
		metrics[workloadType] = &PoolMetrics{
			TotalCreated:  m.TotalCreated,
			TotalAcquired: m.TotalAcquired,
			TotalReleased: m.TotalReleased,
			TotalExpired:  m.TotalExpired,
			CurrentSize:   m.CurrentSize,
			AvailableSize: m.AvailableSize,
			InUseSize:     m.InUseSize,
			HitRate:       m.HitRate,
			AvgWaitTime:   m.AvgWaitTime,
		}
	}

	return metrics
}
