package distributed

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/aether-runtime/aether/internal/scheduler"
	"github.com/aether-runtime/aether/pkg/api"
)

// Skip these tests if Kafka is not available
func skipIfNoKafka(t *testing.T) {
	if os.Getenv("KAFKA_AVAILABLE") != "true" {
		t.Skip("Skipping test: Kafka not available (set KAFKA_AVAILABLE=true to run)")
	}
}

func TestDistributedQueue_EnqueueDequeue(t *testing.T) {
	skipIfNoKafka(t)

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	config := DefaultQueueConfig()
	config.Topic = "test-scheduling-requests"
	config.ConsumerGroup = "test-group"

	dq, err := NewDistributedQueue(logger, config)
	if err != nil {
		t.Fatalf("Failed to create distributed queue: %v", err)
	}
	defer dq.Stop(context.Background())

	ctx := context.Background()

	// Register handler
	received := make(chan *SchedulingRequest, 1)
	dq.RegisterHandler(func(ctx context.Context, req *SchedulingRequest) error {
		received <- req
		return nil
	})

	// Start consumer
	if err := dq.Start(ctx); err != nil {
		t.Fatalf("Failed to start consumer: %v", err)
	}

	// Enqueue request
	agentReq := &scheduler.AgentRequest{
		Config: api.AgentConfig{
			ID:       "agent-001",
			TenantID: "tenant-123",
			Name:     "Test Agent",
			Image:    "python:3.11",
		},
		Resources: scheduler.Resources{
			CPUCores: 2000,
			MemoryMB: 2048,
			DiskMB:   10240,
		},
		Priority:  10,
		CreatedAt: time.Now(),
	}

	if err := dq.Enqueue(ctx, agentReq); err != nil {
		t.Fatalf("Failed to enqueue request: %v", err)
	}

	// Wait for message to be processed
	select {
	case req := <-received:
		if req.AgentID != "agent-001" {
			t.Errorf("AgentID = %s, want agent-001", req.AgentID)
		}
		if req.TenantID != "tenant-123" {
			t.Errorf("TenantID = %s, want tenant-123", req.TenantID)
		}
		if req.Resources.CPUCores != 2000 {
			t.Errorf("CPUCores = %d, want 2000", req.Resources.CPUCores)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Timeout waiting for message")
	}
}

func TestDistributedQueue_Retry(t *testing.T) {
	skipIfNoKafka(t)

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	config := DefaultQueueConfig()
	config.Topic = "test-scheduling-retry"
	config.ConsumerGroup = "test-retry-group"
	config.MaxRetries = 2

	dq, err := NewDistributedQueue(logger, config)
	if err != nil {
		t.Fatalf("Failed to create distributed queue: %v", err)
	}
	defer dq.Stop(context.Background())

	ctx := context.Background()

	// Register handler that fails with retriable error
	attempts := 0
	dq.RegisterHandler(func(ctx context.Context, req *SchedulingRequest) error {
		attempts++
		if attempts < 3 {
			return fmt.Errorf("no suitable node found")
		}
		return nil
	})

	// Start consumer
	if err := dq.Start(ctx); err != nil {
		t.Fatalf("Failed to start consumer: %v", err)
	}

	// Enqueue request
	agentReq := &scheduler.AgentRequest{
		Config: api.AgentConfig{
			ID:       "agent-002",
			TenantID: "tenant-123",
			Name:     "Test Agent",
			Image:    "python:3.11",
		},
		Resources: scheduler.Resources{
			CPUCores: 2000,
			MemoryMB: 2048,
			DiskMB:   10240,
		},
		Priority:  10,
		CreatedAt: time.Now(),
	}

	if err := dq.Enqueue(ctx, agentReq); err != nil {
		t.Fatalf("Failed to enqueue request: %v", err)
	}

	// Wait for retries to complete
	time.Sleep(5 * time.Second)

	// Should have been attempted 3 times (initial + 2 retries)
	if attempts < 3 {
		t.Errorf("Attempts = %d, want at least 3", attempts)
	}
}

func TestDistributedQueue_NonRetriableError(t *testing.T) {
	skipIfNoKafka(t)

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	config := DefaultQueueConfig()
	config.Topic = "test-scheduling-nonretry"
	config.ConsumerGroup = "test-nonretry-group"

	dq, err := NewDistributedQueue(logger, config)
	if err != nil {
		t.Fatalf("Failed to create distributed queue: %v", err)
	}
	defer dq.Stop(context.Background())

	ctx := context.Background()

	// Register handler that fails with non-retriable error
	attempts := 0
	dq.RegisterHandler(func(ctx context.Context, req *SchedulingRequest) error {
		attempts++
		return fmt.Errorf("validation failed: invalid image")
	})

	// Start consumer
	if err := dq.Start(ctx); err != nil {
		t.Fatalf("Failed to start consumer: %v", err)
	}

	// Enqueue request
	agentReq := &scheduler.AgentRequest{
		Config: api.AgentConfig{
			ID:       "agent-003",
			TenantID: "tenant-123",
			Name:     "Test Agent",
			Image:    "invalid:image",
		},
		Resources: scheduler.Resources{
			CPUCores: 2000,
			MemoryMB: 2048,
			DiskMB:   10240,
		},
		Priority:  10,
		CreatedAt: time.Now(),
	}

	if err := dq.Enqueue(ctx, agentReq); err != nil {
		t.Fatalf("Failed to enqueue request: %v", err)
	}

	// Wait for processing
	time.Sleep(3 * time.Second)

	// Should only be attempted once (non-retriable error)
	if attempts != 1 {
		t.Errorf("Attempts = %d, want 1 (non-retriable error)", attempts)
	}
}

func TestDistributedQueue_TenantPartitioning(t *testing.T) {
	skipIfNoKafka(t)

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	config := DefaultQueueConfig()
	config.Topic = "test-scheduling-partition"
	config.ConsumerGroup = "test-partition-group"
	config.PartitionStrategy = "tenant"

	dq, err := NewDistributedQueue(logger, config)
	if err != nil {
		t.Fatalf("Failed to create distributed queue: %v", err)
	}
	defer dq.Stop(context.Background())

	ctx := context.Background()

	// Track requests by tenant
	tenantRequests := make(map[api.TenantID]int)
	mu := sync.RWMutex{}

	dq.RegisterHandler(func(ctx context.Context, req *SchedulingRequest) error {
		mu.Lock()
		defer mu.Unlock()
		tenantRequests[req.TenantID]++
		return nil
	})

	if err := dq.Start(ctx); err != nil {
		t.Fatalf("Failed to start consumer: %v", err)
	}

	// Enqueue requests from multiple tenants
	for i := 0; i < 10; i++ {
		tenantID := fmt.Sprintf("tenant-%d", i%3)
		agentReq := &scheduler.AgentRequest{
			Config: api.AgentConfig{
				ID:       api.AgentID(fmt.Sprintf("agent-%d", i)),
				TenantID: api.TenantID(tenantID),
				Name:     "Test Agent",
				Image:    "python:3.11",
			},
			Resources: scheduler.Resources{
				CPUCores: 2000,
				MemoryMB: 2048,
				DiskMB:   10240,
			},
			Priority:  10,
			CreatedAt: time.Now(),
		}

		if err := dq.Enqueue(ctx, agentReq); err != nil {
			t.Fatalf("Failed to enqueue request %d: %v", i, err)
		}
	}

	// Wait for all messages to be processed
	time.Sleep(5 * time.Second)

	// Verify all requests were processed
	mu.RLock()
	defer mu.RUnlock()

	total := 0
	for _, count := range tenantRequests {
		total += count
	}

	if total != 10 {
		t.Errorf("Total requests processed = %d, want 10", total)
	}
}

func TestDistributedQueue_IsRetriable(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	config := DefaultQueueConfig()
	dq, _ := NewDistributedQueue(logger, config)

	tests := []struct {
		name      string
		err       error
		retriable bool
	}{
		{"No suitable node", fmt.Errorf("no suitable node found"), true},
		{"Insufficient resources", fmt.Errorf("insufficient resources available"), true},
		{"Connection refused", fmt.Errorf("connection refused"), true},
		{"Timeout", fmt.Errorf("request timeout exceeded"), true},
		{"Validation error", fmt.Errorf("validation failed: invalid config"), false},
		{"Not found", fmt.Errorf("tenant not found"), false},
		{"Invalid request", fmt.Errorf("invalid agent configuration"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := dq.isRetriable(tt.err)
			if result != tt.retriable {
				t.Errorf("isRetriable(%v) = %v, want %v", tt.err, result, tt.retriable)
			}
		})
	}
}

func TestSchedulingRequest_Serialization(t *testing.T) {
	req := &SchedulingRequest{
		SchemaVersion: "v1",
		RequestID:     "req-123",
		AgentID:       "agent-001",
		TenantID:      "tenant-123",
		Priority:      10,
		Resources: scheduler.Resources{
			CPUCores: 2000,
			MemoryMB: 2048,
			DiskMB:   10240,
		},
		Constraints: scheduler.SchedulingConstraints{
			NodeSelector: map[string]string{
				"region": "us-west-2",
			},
		},
		Metadata: SchedulingMetadata{
			Image:     "python:3.11",
			CreatedAt: time.Now(),
			Retries:   0,
		},
		TimeoutMS: 30000,
	}

	// Marshal to JSON
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Failed to marshal: %v", err)
	}

	// Unmarshal
	var decoded SchedulingRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	// Verify
	if decoded.RequestID != req.RequestID {
		t.Errorf("RequestID = %s, want %s", decoded.RequestID, req.RequestID)
	}

	if decoded.AgentID != req.AgentID {
		t.Errorf("AgentID = %s, want %s", decoded.AgentID, req.AgentID)
	}

	if decoded.Resources.CPUCores != req.Resources.CPUCores {
		t.Errorf("CPUCores = %d, want %d", decoded.Resources.CPUCores, req.Resources.CPUCores)
	}
}
