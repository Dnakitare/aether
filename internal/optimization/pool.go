package optimization

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

// ConnectionPool manages HTTP connection pooling for external APIs
type ConnectionPool struct {
	logger *slog.Logger
	config ConnectionPoolConfig

	// HTTP clients per API provider
	mu      sync.RWMutex
	clients map[string]*http.Client
	metrics map[string]*ConnectionMetrics
}

// ConnectionPoolConfig configures connection pooling
type ConnectionPoolConfig struct {
	// MaxIdleConns is the maximum number of idle connections
	MaxIdleConns int

	// MaxIdleConnsPerHost is the maximum idle connections per host
	MaxIdleConnsPerHost int

	// MaxConnsPerHost is the maximum connections per host
	MaxConnsPerHost int

	// IdleConnTimeout is how long idle connections are kept
	IdleConnTimeout time.Duration

	// ResponseHeaderTimeout is the timeout for reading response headers
	ResponseHeaderTimeout time.Duration

	// TLSHandshakeTimeout is the timeout for TLS handshakes
	TLSHandshakeTimeout time.Duration

	// ExpectContinueTimeout is the timeout for 100-continue
	ExpectContinueTimeout time.Duration
}

// DefaultConnectionPoolConfig returns default connection pool configuration
func DefaultConnectionPoolConfig() ConnectionPoolConfig {
	return ConnectionPoolConfig{
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   10,
		MaxConnsPerHost:       100,
		IdleConnTimeout:       90 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
}

// ConnectionMetrics tracks connection pool metrics
type ConnectionMetrics struct {
	TotalRequests   int64
	ActiveRequests  int64
	SuccessRequests int64
	FailedRequests  int64
	AvgResponseTime time.Duration
	LastRequestTime time.Time
}

// APIProvider represents different API providers
type APIProvider string

const (
	// ProviderOpenAI represents OpenAI API
	ProviderOpenAI APIProvider = "openai"

	// ProviderAnthropic represents Anthropic API
	ProviderAnthropic APIProvider = "anthropic"

	// ProviderCohere represents Cohere API
	ProviderCohere APIProvider = "cohere"

	// ProviderHuggingFace represents Hugging Face API
	ProviderHuggingFace APIProvider = "huggingface"

	// ProviderCustom represents custom API endpoint
	ProviderCustom APIProvider = "custom"
)

// NewConnectionPool creates a new connection pool
func NewConnectionPool(logger *slog.Logger, config ConnectionPoolConfig) *ConnectionPool {
	return &ConnectionPool{
		logger:  logger,
		config:  config,
		clients: make(map[string]*http.Client),
		metrics: make(map[string]*ConnectionMetrics),
	}
}

// GetClient gets or creates an HTTP client for a provider
func (cp *ConnectionPool) GetClient(provider APIProvider) *http.Client {
	providerKey := string(provider)

	cp.mu.RLock()
	if client, exists := cp.clients[providerKey]; exists {
		cp.mu.RUnlock()
		return client
	}
	cp.mu.RUnlock()

	cp.mu.Lock()
	defer cp.mu.Unlock()

	// Double-check after acquiring write lock
	if client, exists := cp.clients[providerKey]; exists {
		return client
	}

	// Create new client with connection pooling
	transport := &http.Transport{
		MaxIdleConns:          cp.config.MaxIdleConns,
		MaxIdleConnsPerHost:   cp.config.MaxIdleConnsPerHost,
		MaxConnsPerHost:       cp.config.MaxConnsPerHost,
		IdleConnTimeout:       cp.config.IdleConnTimeout,
		ResponseHeaderTimeout: cp.config.ResponseHeaderTimeout,
		TLSHandshakeTimeout:   cp.config.TLSHandshakeTimeout,
		ExpectContinueTimeout: cp.config.ExpectContinueTimeout,
		DisableKeepAlives:     false,
		DisableCompression:    false,
	}

	client := &http.Client{
		Transport: transport,
		Timeout:   60 * time.Second,
	}

	cp.clients[providerKey] = client
	cp.metrics[providerKey] = &ConnectionMetrics{}

	cp.logger.Info("created HTTP client for provider",
		"provider", provider,
		"max_idle_conns", cp.config.MaxIdleConns,
		"max_conns_per_host", cp.config.MaxConnsPerHost,
	)

	return client
}

// Do executes an HTTP request with connection pooling and metrics
func (cp *ConnectionPool) Do(provider APIProvider, req *http.Request) (*http.Response, error) {
	start := time.Now()

	providerKey := string(provider)

	// Update active requests
	cp.updateMetrics(providerKey, func(m *ConnectionMetrics) {
		m.TotalRequests++
		m.ActiveRequests++
	})

	defer func() {
		cp.updateMetrics(providerKey, func(m *ConnectionMetrics) {
			m.ActiveRequests--
		})
	}()

	// Get client
	client := cp.GetClient(provider)

	// Execute request
	// #nosec G107 - URL is validated against node registry, not arbitrary user input
	resp, err := client.Do(req)

	duration := time.Since(start)

	// Update metrics
	cp.updateMetrics(providerKey, func(m *ConnectionMetrics) {
		if err != nil {
			m.FailedRequests++
		} else {
			m.SuccessRequests++
		}
		m.LastRequestTime = time.Now()

		// Update average response time (simple moving average)
		if m.AvgResponseTime == 0 {
			m.AvgResponseTime = duration
		} else {
			m.AvgResponseTime = (m.AvgResponseTime + duration) / 2
		}
	})

	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	cp.logger.Debug("executed HTTP request",
		"provider", provider,
		"method", req.Method,
		"url", req.URL.String(),
		"status", resp.StatusCode,
		"duration", duration,
	)

	return resp, nil
}

// updateMetrics updates metrics for a provider
func (cp *ConnectionPool) updateMetrics(provider string, updateFn func(*ConnectionMetrics)) {
	cp.mu.Lock()
	defer cp.mu.Unlock()

	metrics, exists := cp.metrics[provider]
	if !exists {
		metrics = &ConnectionMetrics{}
		cp.metrics[provider] = metrics
	}

	updateFn(metrics)
}

// GetMetrics returns metrics for all providers
func (cp *ConnectionPool) GetMetrics() map[string]*ConnectionMetrics {
	cp.mu.RLock()
	defer cp.mu.RUnlock()

	metrics := make(map[string]*ConnectionMetrics)
	for provider, m := range cp.metrics {
		metrics[provider] = &ConnectionMetrics{
			TotalRequests:   m.TotalRequests,
			ActiveRequests:  m.ActiveRequests,
			SuccessRequests: m.SuccessRequests,
			FailedRequests:  m.FailedRequests,
			AvgResponseTime: m.AvgResponseTime,
			LastRequestTime: m.LastRequestTime,
		}
	}

	return metrics
}

// Close closes all HTTP clients
func (cp *ConnectionPool) Close() {
	cp.mu.Lock()
	defer cp.mu.Unlock()

	for provider, client := range cp.clients {
		client.CloseIdleConnections()
		cp.logger.Info("closed connections for provider", "provider", provider)
	}

	cp.clients = make(map[string]*http.Client)
}

// WorkloadProfile defines optimization settings for different workload types
type WorkloadProfile struct {
	Name        WorkloadType
	Description string

	// VM Settings
	PrewarmPool      int
	CPULimit         int // millicores
	MemoryLimit      int // MB
	DiskLimit        int // MB
	NetworkBandwidth int // Mbps

	// Connection Settings
	MaxConnections    int
	ConnectionTimeout time.Duration
	IdleTimeout       time.Duration
	KeepAlive         bool

	// Caching Settings
	EnableCache  bool
	CacheTTL     time.Duration
	CacheMaxSize int // MB

	// Checkpointing (for long-running)
	EnableCheckpointing bool
	CheckpointInterval  time.Duration
	CheckpointRetention int
}

// GetWorkloadProfile returns the optimization profile for a workload type
func GetWorkloadProfile(workloadType WorkloadType) WorkloadProfile {
	profiles := map[WorkloadType]WorkloadProfile{
		WorkloadCodeExecution: {
			Name:                WorkloadCodeExecution,
			Description:         "Optimized for fast code execution (Python, Node.js)",
			PrewarmPool:         5,
			CPULimit:            2000,  // 2 vCPU
			MemoryLimit:         4096,  // 4GB
			DiskLimit:           10240, // 10GB
			NetworkBandwidth:    100,
			MaxConnections:      50,
			ConnectionTimeout:   30 * time.Second,
			IdleTimeout:         5 * time.Minute,
			KeepAlive:           false,
			EnableCache:         true,
			CacheTTL:            10 * time.Minute,
			CacheMaxSize:        512,
			EnableCheckpointing: false,
		},
		WorkloadLLMAgent: {
			Name:                WorkloadLLMAgent,
			Description:         "Optimized for LLM agents with API calls",
			PrewarmPool:         3,
			CPULimit:            1000, // 1 vCPU
			MemoryLimit:         2048, // 2GB
			DiskLimit:           5120, // 5GB
			NetworkBandwidth:    1000, // High bandwidth for API calls
			MaxConnections:      100,  // Many concurrent API calls
			ConnectionTimeout:   60 * time.Second,
			IdleTimeout:         10 * time.Minute,
			KeepAlive:           true, // Keep connections alive
			EnableCache:         true,
			CacheTTL:            30 * time.Minute,
			CacheMaxSize:        1024,
			EnableCheckpointing: true,
			CheckpointInterval:  5 * time.Minute,
			CheckpointRetention: 5,
		},
		WorkloadDataProcessing: {
			Name:                WorkloadDataProcessing,
			Description:         "Optimized for data processing workloads",
			PrewarmPool:         2,
			CPULimit:            4000,  // 4 vCPU
			MemoryLimit:         16384, // 16GB
			DiskLimit:           51200, // 50GB
			NetworkBandwidth:    1000,
			MaxConnections:      20,
			ConnectionTimeout:   120 * time.Second,
			IdleTimeout:         30 * time.Minute,
			KeepAlive:           true,
			EnableCache:         true,
			CacheTTL:            60 * time.Minute,
			CacheMaxSize:        4096,
			EnableCheckpointing: true,
			CheckpointInterval:  10 * time.Minute,
			CheckpointRetention: 3,
		},
		WorkloadLongRunning: {
			Name:                WorkloadLongRunning,
			Description:         "Optimized for long-running agents",
			PrewarmPool:         1,
			CPULimit:            2000,  // 2 vCPU
			MemoryLimit:         8192,  // 8GB
			DiskLimit:           20480, // 20GB
			NetworkBandwidth:    500,
			MaxConnections:      50,
			ConnectionTimeout:   300 * time.Second,
			IdleTimeout:         60 * time.Minute,
			KeepAlive:           true,
			EnableCache:         true,
			CacheTTL:            120 * time.Minute,
			CacheMaxSize:        2048,
			EnableCheckpointing: true,
			CheckpointInterval:  15 * time.Minute,
			CheckpointRetention: 10,
		},
	}

	profile, exists := profiles[workloadType]
	if !exists {
		// Return code execution as default
		return profiles[WorkloadCodeExecution]
	}

	return profile
}

// OptimizationManager manages all optimization features
type OptimizationManager struct {
	logger         *slog.Logger
	prewarmingPool *PrewarmingPool
	connectionPool *ConnectionPool
}

// NewOptimizationManager creates a new optimization manager
func NewOptimizationManager(
	logger *slog.Logger,
	prewarmingConfig PrewarmingConfig,
	connectionPoolConfig ConnectionPoolConfig,
) *OptimizationManager {
	return &OptimizationManager{
		logger:         logger,
		prewarmingPool: NewPrewarmingPool(logger, prewarmingConfig),
		connectionPool: NewConnectionPool(logger, connectionPoolConfig),
	}
}

// Start starts all optimization features
func (om *OptimizationManager) Start(ctx context.Context) error {
	om.logger.Info("starting optimization manager")

	// Start pre-warming pool
	if err := om.prewarmingPool.Start(ctx); err != nil {
		return fmt.Errorf("failed to start pre-warming pool: %w", err)
	}

	return nil
}

// GetPrewarmingPool returns the pre-warming pool
func (om *OptimizationManager) GetPrewarmingPool() *PrewarmingPool {
	return om.prewarmingPool
}

// GetConnectionPool returns the connection pool
func (om *OptimizationManager) GetConnectionPool() *ConnectionPool {
	return om.connectionPool
}

// GetMetrics returns all optimization metrics
func (om *OptimizationManager) GetMetrics() map[string]interface{} {
	return map[string]interface{}{
		"prewarming":  om.prewarmingPool.GetMetrics(),
		"connections": om.connectionPool.GetMetrics(),
	}
}

// Close closes all optimization features
func (om *OptimizationManager) Close() {
	om.logger.Info("closing optimization manager")
	om.connectionPool.Close()
}
