package distributed

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/aether-runtime/aether/internal/scheduler"
	"github.com/aether-runtime/aether/pkg/api"
	"github.com/google/uuid"
	"github.com/segmentio/kafka-go"
)

// Queue defines the interface for distributed queue operations.
// Both DistributedQueue (Kafka-based) and MemoryQueue (in-memory) implement this interface.
type Queue interface {
	// RegisterHandler adds a handler for processing scheduling requests
	RegisterHandler(handler RequestHandler)

	// Enqueue adds a scheduling request to the queue
	Enqueue(ctx context.Context, req *scheduler.AgentRequest) error

	// Start begins consuming messages from the queue
	Start(ctx context.Context) error

	// Stop stops the queue consumer
	Stop(ctx context.Context) error

	// Stats returns queue statistics
	Stats(ctx context.Context) (*QueueStats, error)
}

// SchedulingRequest represents a request to schedule an agent.
type SchedulingRequest struct {
	SchemaVersion string                          `json:"schema_version"`
	RequestID     string                          `json:"request_id"`
	AgentID       api.AgentID                     `json:"agent_id"`
	TenantID      api.TenantID                    `json:"tenant_id"`
	Priority      int                             `json:"priority"`
	Resources     scheduler.Resources             `json:"resources"`
	Constraints   scheduler.SchedulingConstraints `json:"constraints"`
	Metadata      SchedulingMetadata              `json:"metadata"`
	TimeoutMS     int64                           `json:"timeout_ms"`
}

// SchedulingMetadata contains additional metadata for a scheduling request.
type SchedulingMetadata struct {
	Image     string    `json:"image"`
	CreatedAt time.Time `json:"created_at"`
	Retries   int       `json:"retries"`
}

// DistributedQueue provides Kafka-based distributed queue operations.
type DistributedQueue struct {
	logger   *slog.Logger
	config   QueueConfig
	writer   *kafka.Writer
	reader   *kafka.Reader
	handlers []RequestHandler
	mu       sync.RWMutex
	stopCh   chan struct{}
	wg       sync.WaitGroup
}

// QueueConfig configures the distributed queue.
type QueueConfig struct {
	// Kafka brokers
	Brokers []string

	// Topic for scheduling requests
	Topic string

	// Dead letter queue topic
	DLQTopic string

	// Consumer group ID
	ConsumerGroup string

	// Number of worker goroutines per partition
	NumWorkers int

	// Max retries before sending to DLQ
	MaxRetries int

	// Request timeout
	RequestTimeout time.Duration

	// Partition key strategy: "tenant" or "round-robin"
	PartitionStrategy string

	// StartFromBeginning controls whether to read from the beginning of the topic
	// Set to true for testing or when you want to process all historical messages
	StartFromBeginning bool
}

// DefaultQueueConfig returns default configuration.
func DefaultQueueConfig() QueueConfig {
	return QueueConfig{
		Brokers:           []string{"localhost:9092"},
		Topic:             "aether.scheduling.requests",
		DLQTopic:          "aether.scheduling.dlq",
		ConsumerGroup:     "aether-schedulers",
		NumWorkers:        10,
		MaxRetries:        3,
		RequestTimeout:    30 * time.Second,
		PartitionStrategy: "tenant",
	}
}

// RequestHandler is a function that processes scheduling requests.
type RequestHandler func(ctx context.Context, req *SchedulingRequest) error

// NewQueue creates a queue with automatic fallback.
// It tries to connect to Kafka first. If Kafka is unavailable, it falls back to an in-memory queue.
// This allows development and testing without requiring Kafka to be running.
func NewQueue(logger *slog.Logger, config QueueConfig) (Queue, error) {
	// Try to connect to Kafka first
	if isKafkaAvailable(config.Brokers) {
		logger.InfoContext(context.Background(), "Kafka available, using distributed queue",
			"brokers", config.Brokers,
		)
		return NewDistributedQueue(logger, config)
	}

	// Fall back to in-memory queue
	logger.WarnContext(context.Background(), "Kafka unavailable, falling back to in-memory queue",
		"brokers", config.Brokers,
		"reason", "development/testing mode",
	)
	return NewMemoryQueue(logger, config), nil
}

// isKafkaAvailable checks if Kafka brokers are reachable.
func isKafkaAvailable(brokers []string) bool {
	if len(brokers) == 0 {
		return false
	}

	// Try to connect to the first broker
	timeout := 2 * time.Second
	conn, err := net.DialTimeout("tcp", brokers[0], timeout)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// NewDistributedQueue creates a new distributed queue.
func NewDistributedQueue(logger *slog.Logger, config QueueConfig) (*DistributedQueue, error) {
	// Create Kafka writer for producing messages
	writer := &kafka.Writer{
		Addr:         kafka.TCP(config.Brokers...),
		Topic:        config.Topic,
		Balancer:     &kafka.Hash{}, // Hash partitioner for consistent routing
		RequiredAcks: kafka.RequireOne,
		Compression:  kafka.Snappy,
		MaxAttempts:  3,
		BatchSize:    100,
		BatchTimeout: 10 * time.Millisecond,
	}

	// Create Kafka reader for consuming messages
	startOffset := kafka.LastOffset
	if config.StartFromBeginning {
		startOffset = kafka.FirstOffset
	}

	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers:        config.Brokers,
		Topic:          config.Topic,
		GroupID:        config.ConsumerGroup,
		MinBytes:       1,
		MaxBytes:       10e6, // 10MB
		MaxWait:        500 * time.Millisecond,
		CommitInterval: 1 * time.Second,
		StartOffset:    startOffset,
		// Cooperative sticky rebalancing for minimal disruption
		GroupBalancers: []kafka.GroupBalancer{
			kafka.RangeGroupBalancer{},
		},
	})

	return &DistributedQueue{
		logger:   logger.With("component", "distributed_queue"),
		config:   config,
		writer:   writer,
		reader:   reader,
		handlers: make([]RequestHandler, 0),
		stopCh:   make(chan struct{}),
	}, nil
}

// RegisterHandler adds a handler for processing scheduling requests.
func (dq *DistributedQueue) RegisterHandler(handler RequestHandler) {
	dq.mu.Lock()
	defer dq.mu.Unlock()
	dq.handlers = append(dq.handlers, handler)
}

// Enqueue adds a scheduling request to the queue.
func (dq *DistributedQueue) Enqueue(ctx context.Context, req *scheduler.AgentRequest) error {
	// Convert to scheduling request
	schedulingReq := &SchedulingRequest{
		SchemaVersion: "v1",
		RequestID:     uuid.New().String(),
		AgentID:       req.Config.ID,
		TenantID:      req.Config.TenantID,
		Priority:      req.Priority,
		Resources:     req.Resources,
		Constraints:   req.Constraints,
		Metadata: SchedulingMetadata{
			Image:     req.Config.Image,
			CreatedAt: req.CreatedAt,
			Retries:   0,
		},
		TimeoutMS: int64(dq.config.RequestTimeout.Milliseconds()),
	}

	// Serialize to JSON
	data, err := json.Marshal(schedulingReq)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	// Determine partition key
	var key []byte
	if dq.config.PartitionStrategy == "tenant" {
		key = []byte(req.Config.TenantID)
	} else {
		// Round-robin: no key
		key = nil
	}

	// Write to Kafka
	message := kafka.Message{
		Key:   key,
		Value: data,
		Headers: []kafka.Header{
			{Key: "request_id", Value: []byte(schedulingReq.RequestID)},
			{Key: "tenant_id", Value: []byte(req.Config.TenantID)},
			{Key: "agent_id", Value: []byte(req.Config.ID)},
		},
	}

	if err := dq.writer.WriteMessages(ctx, message); err != nil {
		return fmt.Errorf("failed to write message to Kafka: %w", err)
	}

	dq.logger.InfoContext(ctx, "enqueued scheduling request",
		"request_id", schedulingReq.RequestID,
		"agent_id", schedulingReq.AgentID,
		"tenant_id", schedulingReq.TenantID,
	)

	return nil
}

// Start begins consuming messages from the queue.
func (dq *DistributedQueue) Start(ctx context.Context) error {
	dq.logger.InfoContext(ctx, "starting distributed queue consumer",
		"topic", dq.config.Topic,
		"consumer_group", dq.config.ConsumerGroup,
		"num_workers", dq.config.NumWorkers,
	)

	// Start worker goroutines
	for i := 0; i < dq.config.NumWorkers; i++ {
		dq.wg.Add(1)
		go dq.worker(ctx, i)
	}

	dq.logger.InfoContext(ctx, "distributed queue consumer started")
	return nil
}

// Stop stops the queue consumer.
func (dq *DistributedQueue) Stop(ctx context.Context) error {
	dq.logger.InfoContext(ctx, "stopping distributed queue consumer")

	close(dq.stopCh)
	dq.wg.Wait()

	if err := dq.reader.Close(); err != nil {
		dq.logger.WarnContext(ctx, "failed to close reader", "error", err)
	}

	if err := dq.writer.Close(); err != nil {
		dq.logger.WarnContext(ctx, "failed to close writer", "error", err)
	}

	dq.logger.InfoContext(ctx, "distributed queue consumer stopped")
	return nil
}

// worker processes messages from the queue.
func (dq *DistributedQueue) worker(ctx context.Context, workerID int) {
	defer dq.wg.Done()

	logger := dq.logger.With("worker_id", workerID)
	logger.InfoContext(ctx, "worker started")

	for {
		select {
		case <-ctx.Done():
			logger.InfoContext(ctx, "worker stopping due to context cancellation")
			return
		case <-dq.stopCh:
			logger.InfoContext(ctx, "worker stopped")
			return
		default:
			// Fetch message with timeout
			fetchCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			message, err := dq.reader.FetchMessage(fetchCtx)
			cancel()

			if err != nil {
				if err == context.DeadlineExceeded || err == context.Canceled {
					continue
				}
				logger.ErrorContext(ctx, "failed to fetch message", "error", err)
				time.Sleep(1 * time.Second)
				continue
			}

			// Process message
			if err := dq.processMessage(ctx, message); err != nil {
				logger.ErrorContext(ctx, "failed to process message",
					"error", err,
					"partition", message.Partition,
					"offset", message.Offset,
				)
				// Don't commit - message will be redelivered
				continue
			}

			// Commit offset on success
			if err := dq.reader.CommitMessages(ctx, message); err != nil {
				logger.WarnContext(ctx, "failed to commit offset", "error", err)
			}
		}
	}
}

// processMessage handles a single message.
func (dq *DistributedQueue) processMessage(ctx context.Context, message kafka.Message) error {
	// Deserialize request
	var req SchedulingRequest
	if err := json.Unmarshal(message.Value, &req); err != nil {
		// Invalid JSON - send to DLQ and commit
		dq.logger.WarnContext(ctx, "invalid message format, sending to DLQ",
			"error", err,
			"offset", message.Offset,
		)
		dq.sendToDLQ(ctx, message, fmt.Errorf("invalid JSON: %w", err))
		return nil // Commit to move past bad message
	}

	dq.logger.DebugContext(ctx, "processing scheduling request",
		"request_id", req.RequestID,
		"agent_id", req.AgentID,
		"retries", req.Metadata.Retries,
	)

	// Create context with timeout
	processCtx, cancel := context.WithTimeout(ctx, time.Duration(req.TimeoutMS)*time.Millisecond)
	defer cancel()

	// Call registered handlers
	dq.mu.RLock()
	handlers := dq.handlers
	dq.mu.RUnlock()

	var lastErr error
	for _, handler := range handlers {
		if err := handler(processCtx, &req); err != nil {
			lastErr = err

			// Check if error is retriable
			if dq.isRetriable(err) && req.Metadata.Retries < dq.config.MaxRetries {
				dq.logger.WarnContext(ctx, "retriable error, will retry",
					"error", err,
					"request_id", req.RequestID,
					"retries", req.Metadata.Retries,
				)
				// Don't commit - message will be redelivered
				return err
			}

			// Non-retriable or max retries exceeded
			dq.logger.ErrorContext(ctx, "non-retriable error or max retries exceeded",
				"error", err,
				"request_id", req.RequestID,
				"retries", req.Metadata.Retries,
			)
			dq.sendToDLQ(ctx, message, err)
			return nil // Commit to move past failed message
		}
	}

	if lastErr != nil {
		return lastErr
	}

	dq.logger.InfoContext(ctx, "scheduling request processed successfully",
		"request_id", req.RequestID,
		"agent_id", req.AgentID,
	)

	return nil
}

// isRetriable checks if an error is retriable.
func (dq *DistributedQueue) isRetriable(err error) bool {
	// Retriable errors:
	// - No suitable node (resources may become available)
	// - Transient network errors
	// - Temporary failures

	errStr := err.Error()

	retriablePatterns := []string{
		"no suitable node",
		"no nodes available",
		"insufficient resources",
		"node capacity changed",
		"temporary failure",
		"connection refused",
		"timeout",
	}

	for _, pattern := range retriablePatterns {
		if contains(errStr, pattern) {
			return true
		}
	}

	return false
}

// sendToDLQ sends a message to the dead letter queue.
func (dq *DistributedQueue) sendToDLQ(ctx context.Context, message kafka.Message, err error) {
	// Create DLQ writer if needed
	dlqWriter := &kafka.Writer{
		Addr:         kafka.TCP(dq.config.Brokers...),
		Topic:        dq.config.DLQTopic,
		RequiredAcks: kafka.RequireOne,
		Compression:  kafka.Snappy,
	}
	defer dlqWriter.Close()

	// Add error information to headers
	headers := append(message.Headers,
		kafka.Header{Key: "error", Value: []byte(err.Error())},
		kafka.Header{Key: "dlq_timestamp", Value: []byte(time.Now().Format(time.RFC3339))},
		kafka.Header{Key: "original_topic", Value: []byte(dq.config.Topic)},
		kafka.Header{Key: "original_partition", Value: []byte(fmt.Sprintf("%d", message.Partition))},
		kafka.Header{Key: "original_offset", Value: []byte(fmt.Sprintf("%d", message.Offset))},
	)

	dlqMessage := kafka.Message{
		Key:     message.Key,
		Value:   message.Value,
		Headers: headers,
	}

	if err := dlqWriter.WriteMessages(ctx, dlqMessage); err != nil {
		dq.logger.ErrorContext(ctx, "failed to write to DLQ",
			"error", err,
			"original_error", err,
		)
	} else {
		dq.logger.InfoContext(ctx, "message sent to DLQ",
			"topic", dq.config.DLQTopic,
			"error", err,
		)
	}
}

// contains checks if a string contains a substring (case-insensitive).
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr ||
		len(s) > len(substr) && findSubstring(s, substr))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// Stats returns queue statistics.
func (dq *DistributedQueue) Stats(ctx context.Context) (*QueueStats, error) {
	// Get reader stats
	stats := dq.reader.Stats()

	return &QueueStats{
		Topic:         dq.config.Topic,
		ConsumerGroup: dq.config.ConsumerGroup,
		Offset:        stats.Offset,
		Lag:           stats.Lag,
		Messages:      stats.Messages,
	}, nil
}

// QueueStats contains queue statistics.
type QueueStats struct {
	Topic         string
	ConsumerGroup string
	Offset        int64
	Lag           int64
	Messages      int64
}
