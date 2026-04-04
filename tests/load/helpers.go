package load

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/dnakitare/aether/internal/scheduler"
	"github.com/dnakitare/aether/internal/scheduler/distributed"
	"github.com/dnakitare/aether/pkg/api"
)

// LoadTestEnvironment provides infrastructure for load tests
type LoadTestEnvironment struct {
	T            *testing.T
	Logger       *slog.Logger
	ShardManager *distributed.ShardManager
	NodeRegistry *distributed.NodeRegistry
	Queue        distributed.Queue // Queue interface supporting both Kafka and in-memory
	Metrics      *LoadTestMetrics
	Nodes        []*scheduler.Node
	SchedulerID  string // ID of the scheduler for this test
	ctx          context.Context
	cancel       context.CancelFunc
}

// LoadTestMetrics tracks performance metrics
type LoadTestMetrics struct {
	mu                 sync.Mutex
	StartTime          time.Time
	EndTime            time.Time
	TotalRequests      int64
	SuccessCount       int64
	FailureCount       int64
	Latencies          []time.Duration
	EnqueueLatencies   []time.Duration
	PlacementLatencies []time.Duration
}

// PerformanceTarget defines expected performance characteristics
type PerformanceTarget struct {
	MaxAvgLatency time.Duration
	MaxP95Latency time.Duration
	MaxP99Latency time.Duration
	MinThroughput float64
}

// SetupLoadTestEnvironment creates a test environment for load testing
func SetupLoadTestEnvironment(t *testing.T, numNodes int) *LoadTestEnvironment {
	t.Helper()

	// Skip if infrastructure not available
	if os.Getenv("DATABASE_URL") == "" {
		t.Skip("Skipping load test: DATABASE_URL not set")
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelWarn, // Reduce noise during load tests
	}))

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)

	// Create shard manager
	shardConfig := distributed.DefaultShardConfig()
	shardConfig.SchedulerID = fmt.Sprintf("load-scheduler-%d", time.Now().Unix())
	shardConfig.InstanceID = fmt.Sprintf("load-instance-%d", time.Now().Unix())
	shardConfig.Hostname = "localhost"
	shardConfig.VirtualNodes = 100
	shardConfig.HeartbeatInterval = 30 * time.Second

	shardManager, err := distributed.NewShardManager(logger, shardConfig)
	if err != nil {
		cancel()
		t.Fatalf("Failed to create shard manager: %v", err)
	}

	if err := shardManager.Start(ctx); err != nil {
		cancel()
		t.Fatalf("Failed to start shard manager: %v", err)
	}

	// Wait for shard manager to initialize
	time.Sleep(2 * time.Second)

	// Create node registry
	nodeRegistryConfig := distributed.DefaultNodeRegistryConfig()
	nodeRegistryConfig.KeyPrefix = fmt.Sprintf("aether:load:%d", time.Now().Unix())
	nodeRegistryConfig.NodeTTL = 300 * time.Second
	nodeRegistryConfig.RedisPassword = os.Getenv("REDIS_PASSWORD")
	if nodeRegistryConfig.RedisPassword == "" {
		nodeRegistryConfig.RedisPassword = "redis_dev_password"
	}

	nodeRegistry, err := distributed.NewNodeRegistry(logger, nodeRegistryConfig)
	if err != nil {
		shardManager.Stop(ctx)
		cancel()
		t.Fatalf("Failed to create node registry: %v", err)
	}

	env := &LoadTestEnvironment{
		T:            t,
		Logger:       logger,
		ShardManager: shardManager,
		NodeRegistry: nodeRegistry,
		Metrics:      NewLoadTestMetrics(),
		Nodes:        make([]*scheduler.Node, 0, numNodes),
		SchedulerID:  shardConfig.SchedulerID,
		ctx:          ctx,
		cancel:       cancel,
	}

	// Register nodes
	env.registerNodes(numNodes, shardConfig.SchedulerID)

	return env
}

// registerNodes creates and registers test nodes
func (e *LoadTestEnvironment) registerNodes(count int, schedulerID string) {
	for i := 1; i <= count; i++ {
		node := &scheduler.Node{
			ID:   fmt.Sprintf("load-node-%04d", i),
			Name: fmt.Sprintf("Load Test Node %d", i),
			Labels: map[string]string{
				"region": "us-west-2",
				"zone":   fmt.Sprintf("us-west-2%c", 'a'+(i-1)%3),
				"env":    "load-test",
			},
			Capacity: scheduler.Resources{
				CPUCores: 200000,  // 200 cores @ 1 core per agent = 200 agents per node
				MemoryMB: 409600,  // 400GB @ 2GB per agent = 200 agents per node
				DiskMB:   4194304, // 4TB
			},
			Allocated: scheduler.Resources{},
			Agents:    make(map[api.AgentID]*scheduler.AgentAllocation),
		}

		if err := e.NodeRegistry.RegisterNode(e.ctx, node, schedulerID); err != nil {
			e.T.Fatalf("Failed to register node %d: %v", i, err)
		}

		e.Nodes = append(e.Nodes, node)
	}

	e.T.Logf("Registered %d nodes for load testing", count)
}

// SetupQueue creates and starts a queue for load testing.
// Automatically uses Kafka if available, otherwise falls back to in-memory queue.
func (e *LoadTestEnvironment) SetupQueue(topicSuffix string, numWorkers int) {
	queueConfig := distributed.DefaultQueueConfig()
	queueConfig.Topic = fmt.Sprintf("load-test-%s", topicSuffix)
	queueConfig.ConsumerGroup = fmt.Sprintf("load-test-%s-group", topicSuffix)
	queueConfig.NumWorkers = numWorkers
	queueConfig.StartFromBeginning = true

	queue, err := distributed.NewQueue(e.Logger, queueConfig)
	if err != nil {
		e.TearDown()
		e.T.Fatalf("Failed to create queue: %v", err)
	}

	e.Queue = queue

	// Start queue
	if err := e.Queue.Start(e.ctx); err != nil {
		e.TearDown()
		e.T.Fatalf("Failed to start queue: %v", err)
	}

	// Wait for queue to initialize
	time.Sleep(2 * time.Second)
}

// TearDown cleans up the test environment
func (e *LoadTestEnvironment) TearDown() {
	if e.Queue != nil {
		e.Queue.Stop(e.ctx)
	}
	if e.NodeRegistry != nil {
		e.NodeRegistry.Close()
	}
	if e.ShardManager != nil {
		e.ShardManager.Stop(e.ctx)
	}
	if e.cancel != nil {
		e.cancel()
	}
}

// NewLoadTestMetrics creates a new metrics collector
func NewLoadTestMetrics() *LoadTestMetrics {
	return &LoadTestMetrics{
		StartTime:          time.Now(),
		Latencies:          make([]time.Duration, 0, 10000),
		EnqueueLatencies:   make([]time.Duration, 0, 10000),
		PlacementLatencies: make([]time.Duration, 0, 10000),
	}
}

// RecordSuccess records a successful operation
func (m *LoadTestMetrics) RecordSuccess(latency time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.SuccessCount++
	m.TotalRequests++
	m.Latencies = append(m.Latencies, latency)
}

// RecordFailure records a failed operation
func (m *LoadTestMetrics) RecordFailure() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.FailureCount++
	m.TotalRequests++
}

// RecordEnqueue records enqueue latency
func (m *LoadTestMetrics) RecordEnqueue(latency time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.EnqueueLatencies = append(m.EnqueueLatencies, latency)
}

// RecordPlacement records placement latency
func (m *LoadTestMetrics) RecordPlacement(latency time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.PlacementLatencies = append(m.PlacementLatencies, latency)
}

// Finalize marks the end of the test
func (m *LoadTestMetrics) Finalize() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.EndTime = time.Now()
}

// Report generates a performance report
type PerformanceReport struct {
	Duration            time.Duration
	TotalRequests       int64
	SuccessCount        int64
	FailureCount        int64
	SuccessRate         float64
	Throughput          float64
	AvgLatency          time.Duration
	P50Latency          time.Duration
	P95Latency          time.Duration
	P99Latency          time.Duration
	AvgEnqueueLatency   time.Duration
	AvgPlacementLatency time.Duration
}

// GetReport generates a performance report from collected metrics
func (m *LoadTestMetrics) GetReport() *PerformanceReport {
	m.mu.Lock()
	defer m.mu.Unlock()

	duration := m.EndTime.Sub(m.StartTime)
	if duration == 0 {
		duration = time.Since(m.StartTime)
	}

	report := &PerformanceReport{
		Duration:      duration,
		TotalRequests: m.TotalRequests,
		SuccessCount:  m.SuccessCount,
		FailureCount:  m.FailureCount,
	}

	if m.TotalRequests > 0 {
		report.SuccessRate = float64(m.SuccessCount) / float64(m.TotalRequests) * 100
		report.Throughput = float64(m.SuccessCount) / duration.Seconds()
	}

	if len(m.Latencies) > 0 {
		sorted := make([]time.Duration, len(m.Latencies))
		copy(sorted, m.Latencies)
		sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })

		var sum time.Duration
		for _, lat := range sorted {
			sum += lat
		}
		report.AvgLatency = sum / time.Duration(len(sorted))
		report.P50Latency = sorted[len(sorted)*50/100]
		report.P95Latency = sorted[len(sorted)*95/100]
		report.P99Latency = sorted[len(sorted)*99/100]
	}

	if len(m.EnqueueLatencies) > 0 {
		var sum time.Duration
		for _, lat := range m.EnqueueLatencies {
			sum += lat
		}
		report.AvgEnqueueLatency = sum / time.Duration(len(m.EnqueueLatencies))
	}

	if len(m.PlacementLatencies) > 0 {
		var sum time.Duration
		for _, lat := range m.PlacementLatencies {
			sum += lat
		}
		report.AvgPlacementLatency = sum / time.Duration(len(m.PlacementLatencies))
	}

	return report
}

// Print outputs the report to the test log
func (r *PerformanceReport) Print(t *testing.T) {
	t.Logf("\n=== PERFORMANCE REPORT ===")
	t.Logf("Duration:              %v", r.Duration)
	t.Logf("Total Requests:        %d", r.TotalRequests)
	t.Logf("Successful:            %d", r.SuccessCount)
	t.Logf("Failed:                %d", r.FailureCount)
	t.Logf("Success Rate:          %.2f%%", r.SuccessRate)
	t.Logf("Throughput:            %.2f ops/sec", r.Throughput)
	t.Logf("")
	t.Logf("Latency Statistics:")
	t.Logf("  Average:             %v", r.AvgLatency)
	t.Logf("  P50:                 %v", r.P50Latency)
	t.Logf("  P95:                 %v", r.P95Latency)
	t.Logf("  P99:                 %v", r.P99Latency)
	if r.AvgEnqueueLatency > 0 {
		t.Logf("  Avg Enqueue:         %v", r.AvgEnqueueLatency)
	}
	if r.AvgPlacementLatency > 0 {
		t.Logf("  Avg Placement:       %v", r.AvgPlacementLatency)
	}
	t.Logf("========================\n")
}

// ValidatePerformance checks if metrics meet performance targets
func (r *PerformanceReport) ValidatePerformance(t *testing.T, target *PerformanceTarget) {
	t.Helper()

	passed := true

	if r.AvgLatency > target.MaxAvgLatency {
		t.Errorf("Average latency %v exceeds target %v", r.AvgLatency, target.MaxAvgLatency)
		passed = false
	}

	if r.P95Latency > target.MaxP95Latency {
		t.Errorf("P95 latency %v exceeds target %v", r.P95Latency, target.MaxP95Latency)
		passed = false
	}

	if r.P99Latency > target.MaxP99Latency {
		t.Errorf("P99 latency %v exceeds target %v", r.P99Latency, target.MaxP99Latency)
		passed = false
	}

	if r.Throughput < target.MinThroughput {
		t.Errorf("Throughput %.2f ops/sec below target %.2f ops/sec", r.Throughput, target.MinThroughput)
		passed = false
	}

	if passed {
		t.Logf("✅ All performance targets met!")
	}
}

// CreateTestAgentRequest creates a test agent request
func CreateTestAgentRequest(agentNum int, tenantID string) *scheduler.AgentRequest {
	return &scheduler.AgentRequest{
		Config: api.AgentConfig{
			ID:       api.AgentID(fmt.Sprintf("load-agent-%06d", agentNum)),
			TenantID: api.TenantID(tenantID),
			Name:     fmt.Sprintf("Load Test Agent %d", agentNum),
			Image:    "python:3.11",
		},
		Resources: scheduler.Resources{
			CPUCores: 1000,  // 1 core
			MemoryMB: 2048,  // 2GB
			DiskMB:   10240, // 10GB
		},
		Priority:  10,
		CreatedAt: time.Now(),
	}
}
