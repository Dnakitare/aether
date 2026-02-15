package distributed

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/aether-runtime/aether/internal/scheduler"
	"github.com/google/uuid"
)

// MemoryQueue provides an in-memory queue for testing and development.
// It implements the same interface as DistributedQueue but uses Go channels
// instead of Kafka, making it suitable for environments without Kafka.
type MemoryQueue struct {
	logger   *slog.Logger
	config   QueueConfig
	handlers []RequestHandler
	queue    chan *SchedulingRequest
	mu       sync.RWMutex
	stopCh   chan struct{}
	wg       sync.WaitGroup
	stats    *memoryQueueStats
}

type memoryQueueStats struct {
	mu         sync.Mutex
	enqueued   int64
	processed  int64
	failed     int64
	dlq        []*SchedulingRequest
	maxDLQSize int
}

// NewMemoryQueue creates a new in-memory queue.
func NewMemoryQueue(logger *slog.Logger, config QueueConfig) *MemoryQueue {
	// Use a buffered channel to allow some queueing
	queueSize := config.NumWorkers * 100
	if queueSize < 1000 {
		queueSize = 1000
	}

	return &MemoryQueue{
		logger:   logger.With("component", "memory_queue", "impl", "in-memory"),
		config:   config,
		handlers: make([]RequestHandler, 0),
		queue:    make(chan *SchedulingRequest, queueSize),
		stopCh:   make(chan struct{}),
		stats: &memoryQueueStats{
			dlq:        make([]*SchedulingRequest, 0),
			maxDLQSize: 1000,
		},
	}
}

// RegisterHandler adds a handler for processing scheduling requests.
func (mq *MemoryQueue) RegisterHandler(handler RequestHandler) {
	mq.mu.Lock()
	defer mq.mu.Unlock()
	mq.handlers = append(mq.handlers, handler)
}

// Enqueue adds a scheduling request to the queue.
func (mq *MemoryQueue) Enqueue(ctx context.Context, req *scheduler.AgentRequest) error {
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
		TimeoutMS: int64(mq.config.RequestTimeout.Milliseconds()),
	}

	// Try to enqueue with timeout
	select {
	case mq.queue <- schedulingReq:
		mq.stats.mu.Lock()
		mq.stats.enqueued++
		mq.stats.mu.Unlock()

		mq.logger.DebugContext(ctx, "enqueued scheduling request",
			"request_id", schedulingReq.RequestID,
			"agent_id", schedulingReq.AgentID,
			"tenant_id", schedulingReq.TenantID,
			"queue_len", len(mq.queue),
		)
		return nil

	case <-ctx.Done():
		return fmt.Errorf("enqueue cancelled: %w", ctx.Err())

	case <-time.After(5 * time.Second):
		return fmt.Errorf("enqueue timeout: queue full (%d items)", len(mq.queue))
	}
}

// Start begins consuming messages from the queue.
func (mq *MemoryQueue) Start(ctx context.Context) error {
	mq.logger.InfoContext(ctx, "starting memory queue consumer",
		"num_workers", mq.config.NumWorkers,
		"queue_capacity", cap(mq.queue),
	)

	// Start worker goroutines
	for i := 0; i < mq.config.NumWorkers; i++ {
		mq.wg.Add(1)
		go mq.worker(ctx, i)
	}

	mq.logger.InfoContext(ctx, "memory queue consumer started")
	return nil
}

// Stop stops the queue consumer.
func (mq *MemoryQueue) Stop(ctx context.Context) error {
	mq.logger.InfoContext(ctx, "stopping memory queue consumer")

	close(mq.stopCh)

	// Wait for workers to finish with timeout
	done := make(chan struct{})
	go func() {
		mq.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		mq.logger.InfoContext(ctx, "all workers stopped gracefully")
	case <-time.After(10 * time.Second):
		mq.logger.WarnContext(ctx, "timeout waiting for workers to stop")
	}

	// Drain remaining queue items
	remaining := len(mq.queue)
	if remaining > 0 {
		mq.logger.WarnContext(ctx, "queue had unprocessed items",
			"count", remaining,
		)
	}

	mq.logger.InfoContext(ctx, "memory queue consumer stopped",
		"total_enqueued", mq.stats.enqueued,
		"total_processed", mq.stats.processed,
		"total_failed", mq.stats.failed,
	)

	return nil
}

// worker processes messages from the queue.
func (mq *MemoryQueue) worker(ctx context.Context, workerID int) {
	defer mq.wg.Done()

	logger := mq.logger.With("worker_id", workerID)
	logger.InfoContext(ctx, "worker started")

	for {
		select {
		case <-ctx.Done():
			logger.InfoContext(ctx, "worker stopping due to context cancellation")
			return

		case <-mq.stopCh:
			logger.InfoContext(ctx, "worker stopped")
			return

		case req := <-mq.queue:
			// Process the request
			if err := mq.processRequest(ctx, req); err != nil {
				logger.ErrorContext(ctx, "failed to process request",
					"error", err,
					"request_id", req.RequestID,
					"agent_id", req.AgentID,
					"retries", req.Metadata.Retries,
				)

				// Handle retries
				if mq.isRetriable(err) && req.Metadata.Retries < mq.config.MaxRetries {
					req.Metadata.Retries++
					logger.WarnContext(ctx, "requeueing request for retry",
						"request_id", req.RequestID,
						"retries", req.Metadata.Retries,
					)

					// Retry after a short delay
					go func(r *SchedulingRequest) {
						time.Sleep(time.Duration(r.Metadata.Retries) * time.Second)
						select {
						case mq.queue <- r:
							// Re-enqueued successfully
						case <-time.After(5 * time.Second):
							// Failed to re-enqueue, send to DLQ
							mq.sendToDLQ(r, fmt.Errorf("failed to re-enqueue after %d retries", r.Metadata.Retries))
						}
					}(req)
				} else {
					// Max retries exceeded or non-retriable error
					mq.sendToDLQ(req, err)
					mq.stats.mu.Lock()
					mq.stats.failed++
					mq.stats.mu.Unlock()
				}
			} else {
				mq.stats.mu.Lock()
				mq.stats.processed++
				mq.stats.mu.Unlock()
			}
		}
	}
}

// processRequest handles a single request.
func (mq *MemoryQueue) processRequest(ctx context.Context, req *SchedulingRequest) error {
	mq.logger.DebugContext(ctx, "processing scheduling request",
		"request_id", req.RequestID,
		"agent_id", req.AgentID,
		"retries", req.Metadata.Retries,
	)

	// Create context with timeout
	processCtx, cancel := context.WithTimeout(ctx, time.Duration(req.TimeoutMS)*time.Millisecond)
	defer cancel()

	// Call registered handlers
	mq.mu.RLock()
	handlers := mq.handlers
	mq.mu.RUnlock()

	var lastErr error
	for _, handler := range handlers {
		if err := handler(processCtx, req); err != nil {
			lastErr = err
			break
		}
	}

	if lastErr != nil {
		return lastErr
	}

	mq.logger.InfoContext(ctx, "scheduling request processed successfully",
		"request_id", req.RequestID,
		"agent_id", req.AgentID,
	)

	return nil
}

// isRetriable checks if an error is retriable.
func (mq *MemoryQueue) isRetriable(err error) bool {
	errStr := err.Error()

	retriablePatterns := []string{
		"no suitable node",
		"no nodes available",
		"insufficient resources",
		"node capacity changed",
		"temporary failure",
		"timeout",
	}

	for _, pattern := range retriablePatterns {
		if contains(errStr, pattern) {
			return true
		}
	}

	return false
}

// sendToDLQ adds a failed request to the in-memory dead letter queue.
func (mq *MemoryQueue) sendToDLQ(req *SchedulingRequest, err error) {
	mq.stats.mu.Lock()
	defer mq.stats.mu.Unlock()

	// Limit DLQ size
	if len(mq.stats.dlq) >= mq.stats.maxDLQSize {
		// Remove oldest item
		mq.stats.dlq = mq.stats.dlq[1:]
	}

	mq.stats.dlq = append(mq.stats.dlq, req)

	mq.logger.WarnContext(context.Background(), "request sent to DLQ",
		"request_id", req.RequestID,
		"agent_id", req.AgentID,
		"error", err.Error(),
		"dlq_size", len(mq.stats.dlq),
	)
}

// Stats returns queue statistics.
func (mq *MemoryQueue) Stats(ctx context.Context) (*QueueStats, error) {
	mq.stats.mu.Lock()
	defer mq.stats.mu.Unlock()

	return &QueueStats{
		Topic:         "memory-queue",
		ConsumerGroup: "in-memory",
		Offset:        mq.stats.processed,
		Lag:           int64(len(mq.queue)),
		Messages:      mq.stats.enqueued,
	}, nil
}

// GetDLQ returns the current dead letter queue contents (for testing/debugging).
func (mq *MemoryQueue) GetDLQ() []*SchedulingRequest {
	mq.stats.mu.Lock()
	defer mq.stats.mu.Unlock()

	// Return a copy
	dlq := make([]*SchedulingRequest, len(mq.stats.dlq))
	copy(dlq, mq.stats.dlq)
	return dlq
}

// QueueDepth returns the current queue depth.
func (mq *MemoryQueue) QueueDepth() int {
	return len(mq.queue)
}
