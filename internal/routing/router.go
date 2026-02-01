// Package routing provides network routing and traffic policies for agent communication.
package routing

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/dnakitare/aether/pkg/api"
)

// Router provides service discovery and routing for agents.
type Router struct {
	logger   *slog.Logger
	mu       sync.RWMutex
	registry map[api.AgentID]*ServiceEntry
	config   RouterConfig
}

// RouterConfig holds router configuration.
type RouterConfig struct {
	// HealthCheckInterval for service health checks
	HealthCheckInterval time.Duration

	// HealthCheckTimeout for health check requests
	HealthCheckTimeout time.Duration

	// UnhealthyThreshold for marking service unhealthy
	UnhealthyThreshold int

	// EnableCircuitBreaker enables circuit breaker pattern
	EnableCircuitBreaker bool
}

// DefaultRouterConfig returns default router configuration.
func DefaultRouterConfig() RouterConfig {
	return RouterConfig{
		HealthCheckInterval:  30 * time.Second,
		HealthCheckTimeout:   5 * time.Second,
		UnhealthyThreshold:   3,
		EnableCircuitBreaker: true,
	}
}

// ServiceEntry represents a registered service (agent).
type ServiceEntry struct {
	AgentID    api.AgentID
	TenantID   api.TenantID
	Address    string
	Port       int
	Metadata   map[string]string
	Health     HealthStatus
	LastCheck  time.Time
	FailCount  int
	Registered time.Time
}

// HealthStatus represents service health.
type HealthStatus string

const (
	HealthHealthy   HealthStatus = "healthy"
	HealthUnhealthy HealthStatus = "unhealthy"
	HealthUnknown   HealthStatus = "unknown"
)

// NewRouter creates a new router.
func NewRouter(logger *slog.Logger, config RouterConfig) *Router {
	return &Router{
		logger:   logger.With("component", "router"),
		registry: make(map[api.AgentID]*ServiceEntry),
		config:   config,
	}
}

// Register registers an agent service.
func (r *Router) Register(ctx context.Context, agentID api.AgentID, tenantID api.TenantID, address string, port int, metadata map[string]string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	entry := &ServiceEntry{
		AgentID:    agentID,
		TenantID:   tenantID,
		Address:    address,
		Port:       port,
		Metadata:   metadata,
		Health:     HealthUnknown,
		Registered: time.Now(),
	}

	r.registry[agentID] = entry

	r.logger.InfoContext(ctx, "service registered",
		"agent_id", agentID,
		"address", address,
		"port", port,
	)

	return nil
}

// Deregister removes an agent service.
func (r *Router) Deregister(ctx context.Context, agentID api.AgentID) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.registry, agentID)

	r.logger.InfoContext(ctx, "service deregistered", "agent_id", agentID)
	return nil
}

// Lookup finds a service by agent ID.
func (r *Router) Lookup(agentID api.AgentID) (*ServiceEntry, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	entry, exists := r.registry[agentID]
	if !exists {
		return nil, fmt.Errorf("service not found: %s", agentID)
	}

	return entry, nil
}

// ListServices lists all registered services.
func (r *Router) ListServices() []*ServiceEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()

	services := make([]*ServiceEntry, 0, len(r.registry))
	for _, entry := range r.registry {
		services = append(services, entry)
	}

	return services
}

// ListHealthyServices lists all healthy services for a tenant.
func (r *Router) ListHealthyServices(tenantID api.TenantID) []*ServiceEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var services []*ServiceEntry
	for _, entry := range r.registry {
		if entry.TenantID == tenantID && entry.Health == HealthHealthy {
			services = append(services, entry)
		}
	}

	return services
}

// UpdateHealth updates the health status of a service.
func (r *Router) UpdateHealth(agentID api.AgentID, healthy bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	entry, exists := r.registry[agentID]
	if !exists {
		return
	}

	entry.LastCheck = time.Now()

	if healthy {
		entry.Health = HealthHealthy
		entry.FailCount = 0
	} else {
		entry.FailCount++
		if entry.FailCount >= r.config.UnhealthyThreshold {
			entry.Health = HealthUnhealthy
		}
	}
}

// LoadBalancer provides load balancing across agent replicas.
type LoadBalancer struct {
	logger   *slog.Logger
	router   *Router
	strategy LoadBalancingStrategy
}

// LoadBalancingStrategy defines load balancing algorithm.
type LoadBalancingStrategy string

const (
	// StrategyRoundRobin distributes requests evenly
	StrategyRoundRobin LoadBalancingStrategy = "round_robin"

	// StrategyLeastConnections sends to agent with fewest connections
	StrategyLeastConnections LoadBalancingStrategy = "least_connections"

	// StrategyRandom selects a random healthy agent
	StrategyRandom LoadBalancingStrategy = "random"
)

// NewLoadBalancer creates a new load balancer.
func NewLoadBalancer(logger *slog.Logger, router *Router, strategy LoadBalancingStrategy) *LoadBalancer {
	return &LoadBalancer{
		logger:   logger.With("component", "load_balancer"),
		router:   router,
		strategy: strategy,
	}
}

// SelectAgent selects an agent using the configured strategy.
func (lb *LoadBalancer) SelectAgent(tenantID api.TenantID) (*ServiceEntry, error) {
	services := lb.router.ListHealthyServices(tenantID)
	if len(services) == 0 {
		return nil, fmt.Errorf("no healthy services available for tenant %s", tenantID)
	}

	switch lb.strategy {
	case StrategyRoundRobin:
		// Simple round-robin (in production, would track index)
		return services[0], nil
	case StrategyRandom:
		// Random selection
		return services[time.Now().UnixNano()%int64(len(services))], nil
	default:
		return services[0], nil
	}
}

// TrafficPolicy defines traffic management policies.
type TrafficPolicy struct {
	// Retry configuration
	MaxRetries  int
	RetryDelay  time.Duration
	RetryOnCode []int

	// Timeout configuration
	ConnectionTimeout time.Duration
	RequestTimeout    time.Duration

	// Circuit breaker configuration
	FailureThreshold int
	SuccessThreshold int
	Timeout          time.Duration
}

// DefaultTrafficPolicy returns default traffic policy.
func DefaultTrafficPolicy() TrafficPolicy {
	return TrafficPolicy{
		MaxRetries:        3,
		RetryDelay:        100 * time.Millisecond,
		RetryOnCode:       []int{500, 502, 503, 504},
		ConnectionTimeout: 5 * time.Second,
		RequestTimeout:    30 * time.Second,
		FailureThreshold:  5,
		SuccessThreshold:  2,
		Timeout:           60 * time.Second,
	}
}

// CircuitBreaker implements the circuit breaker pattern.
type CircuitBreaker struct {
	logger       *slog.Logger
	mu           sync.RWMutex
	state        CircuitState
	failures     int
	successes    int
	lastFailTime time.Time
	policy       TrafficPolicy
}

// CircuitState represents circuit breaker state.
type CircuitState string

const (
	StateClosed   CircuitState = "closed"    // Normal operation
	StateOpen     CircuitState = "open"      // Failing, reject requests
	StateHalfOpen CircuitState = "half_open" // Testing if service recovered
)

// NewCircuitBreaker creates a new circuit breaker.
func NewCircuitBreaker(logger *slog.Logger, policy TrafficPolicy) *CircuitBreaker {
	return &CircuitBreaker{
		logger: logger.With("component", "circuit_breaker"),
		state:  StateClosed,
		policy: policy,
	}
}

// Allow checks if a request is allowed through the circuit breaker.
func (cb *CircuitBreaker) Allow() error {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	switch cb.state {
	case StateClosed:
		return nil
	case StateOpen:
		// Check if timeout has elapsed
		if time.Since(cb.lastFailTime) > cb.policy.Timeout {
			cb.mu.RUnlock()
			cb.mu.Lock()
			cb.state = StateHalfOpen
			cb.successes = 0
			cb.mu.Unlock()
			cb.mu.RLock()
			return nil
		}
		return fmt.Errorf("circuit breaker is open")
	case StateHalfOpen:
		return nil
	default:
		return nil
	}
}

// RecordSuccess records a successful request.
func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if cb.state == StateHalfOpen {
		cb.successes++
		if cb.successes >= cb.policy.SuccessThreshold {
			cb.logger.Info("circuit breaker closed after recovery")
			cb.state = StateClosed
			cb.failures = 0
		}
	} else if cb.state == StateClosed {
		cb.failures = 0
	}
}

// RecordFailure records a failed request.
func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failures++
	cb.lastFailTime = time.Now()

	if cb.state == StateHalfOpen {
		cb.logger.Warn("circuit breaker opened from half-open state")
		cb.state = StateOpen
	} else if cb.state == StateClosed && cb.failures >= cb.policy.FailureThreshold {
		cb.logger.Warn("circuit breaker opened", "failures", cb.failures)
		cb.state = StateOpen
	}
}

// GetState returns the current circuit breaker state.
func (cb *CircuitBreaker) GetState() CircuitState {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.state
}

// ConnectionPool manages connections to agents.
type ConnectionPool struct {
	logger *slog.Logger
	mu     sync.RWMutex
	pools  map[api.AgentID]*AgentPool
	config ConnectionPoolConfig
}

// ConnectionPoolConfig holds connection pool configuration.
type ConnectionPoolConfig struct {
	// MaxConnections per agent
	MaxConnections int

	// IdleTimeout for idle connections
	IdleTimeout time.Duration

	// ConnectionTimeout for establishing connections
	ConnectionTimeout time.Duration
}

// DefaultConnectionPoolConfig returns default connection pool configuration.
func DefaultConnectionPoolConfig() ConnectionPoolConfig {
	return ConnectionPoolConfig{
		MaxConnections:    100,
		IdleTimeout:       5 * time.Minute,
		ConnectionTimeout: 5 * time.Second,
	}
}

// AgentPool represents a connection pool for a specific agent.
type AgentPool struct {
	agentID     api.AgentID
	activeConns int
	idleConns   int
	lastUsed    time.Time
}

// NewConnectionPool creates a new connection pool.
func NewConnectionPool(logger *slog.Logger, config ConnectionPoolConfig) *ConnectionPool {
	return &ConnectionPool{
		logger: logger.With("component", "connection_pool"),
		pools:  make(map[api.AgentID]*AgentPool),
		config: config,
	}
}

// GetConnection gets or creates a connection to an agent.
func (cp *ConnectionPool) GetConnection(agentID api.AgentID) error {
	cp.mu.Lock()
	defer cp.mu.Unlock()

	pool, exists := cp.pools[agentID]
	if !exists {
		pool = &AgentPool{
			agentID: agentID,
		}
		cp.pools[agentID] = pool
	}

	if pool.activeConns >= cp.config.MaxConnections {
		return fmt.Errorf("connection pool exhausted for agent %s", agentID)
	}

	pool.activeConns++
	pool.lastUsed = time.Now()

	return nil
}

// ReleaseConnection releases a connection back to the pool.
func (cp *ConnectionPool) ReleaseConnection(agentID api.AgentID) {
	cp.mu.Lock()
	defer cp.mu.Unlock()

	pool, exists := cp.pools[agentID]
	if !exists {
		return
	}

	if pool.activeConns > 0 {
		pool.activeConns--
		pool.idleConns++
	}
}

// CleanupIdle removes idle connections.
func (cp *ConnectionPool) CleanupIdle() {
	cp.mu.Lock()
	defer cp.mu.Unlock()

	now := time.Now()
	for agentID, pool := range cp.pools {
		if now.Sub(pool.lastUsed) > cp.config.IdleTimeout {
			delete(cp.pools, agentID)
			cp.logger.Debug("removed idle connection pool", "agent_id", agentID)
		}
	}
}
