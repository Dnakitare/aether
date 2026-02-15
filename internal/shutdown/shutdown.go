package shutdown

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

// Manager manages graceful shutdown of the application.
type Manager struct {
	logger   *slog.Logger
	signals  chan os.Signal
	hooks    []Hook
	mu       sync.Mutex
	timeout  time.Duration
	shutdown bool
}

// Hook is a function that is called during shutdown.
type Hook struct {
	Name     string
	Priority int // Lower priority runs first
	Func     func(ctx context.Context) error
}

// Config configures the shutdown manager.
type Config struct {
	// Timeout is the maximum time to wait for graceful shutdown
	Timeout time.Duration

	// Signals to listen for (defaults to SIGINT, SIGTERM)
	Signals []os.Signal
}

// DefaultConfig returns default shutdown configuration.
func DefaultConfig() Config {
	return Config{
		Timeout: 30 * time.Second,
		Signals: []os.Signal{syscall.SIGINT, syscall.SIGTERM},
	}
}

// NewManager creates a new shutdown manager.
func NewManager(logger *slog.Logger, config Config) *Manager {
	if config.Timeout == 0 {
		config.Timeout = 30 * time.Second
	}

	if len(config.Signals) == 0 {
		config.Signals = []os.Signal{syscall.SIGINT, syscall.SIGTERM}
	}

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, config.Signals...)

	return &Manager{
		logger:  logger,
		signals: signals,
		hooks:   make([]Hook, 0),
		timeout: config.Timeout,
	}
}

// RegisterHook registers a shutdown hook.
// Hooks are executed in order of priority (lower priority runs first).
func (m *Manager) RegisterHook(name string, priority int, fn func(ctx context.Context) error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.hooks = append(m.hooks, Hook{
		Name:     name,
		Priority: priority,
		Func:     fn,
	})
}

// Wait waits for a shutdown signal and executes all registered hooks.
// Returns an error if any hook fails or if shutdown times out.
func (m *Manager) Wait() error {
	// Wait for shutdown signal
	sig := <-m.signals
	m.logger.Info("received shutdown signal", "signal", sig)

	return m.Shutdown(context.Background())
}

// Shutdown initiates graceful shutdown immediately.
func (m *Manager) Shutdown(ctx context.Context) error {
	m.mu.Lock()
	if m.shutdown {
		m.mu.Unlock()
		return fmt.Errorf("shutdown already in progress")
	}
	m.shutdown = true
	m.mu.Unlock()

	m.logger.Info("initiating graceful shutdown", "timeout", m.timeout)

	// Create context with timeout
	shutdownCtx, cancel := context.WithTimeout(ctx, m.timeout)
	defer cancel()

	// Sort hooks by priority
	hooks := make([]Hook, len(m.hooks))
	copy(hooks, m.hooks)

	// Simple bubble sort by priority
	for i := 0; i < len(hooks); i++ {
		for j := i + 1; j < len(hooks); j++ {
			if hooks[j].Priority < hooks[i].Priority {
				hooks[i], hooks[j] = hooks[j], hooks[i]
			}
		}
	}

	// Execute hooks in order
	var errors []error
	for _, hook := range hooks {
		m.logger.Info("executing shutdown hook", "name", hook.Name, "priority", hook.Priority)
		start := time.Now()

		if err := hook.Func(shutdownCtx); err != nil {
			m.logger.Error("shutdown hook failed",
				"name", hook.Name,
				"error", err,
				"duration", time.Since(start),
			)
			errors = append(errors, fmt.Errorf("%s: %w", hook.Name, err))
		} else {
			m.logger.Info("shutdown hook completed",
				"name", hook.Name,
				"duration", time.Since(start),
			)
		}

		// Check if context is cancelled
		select {
		case <-shutdownCtx.Done():
			m.logger.Warn("shutdown timeout reached, forcing exit")
			return fmt.Errorf("shutdown timeout: %w", shutdownCtx.Err())
		default:
		}
	}

	if len(errors) > 0 {
		m.logger.Error("shutdown completed with errors", "error_count", len(errors))
		return fmt.Errorf("shutdown errors: %v", errors)
	}

	m.logger.Info("graceful shutdown completed successfully")
	return nil
}

// IsShuttingDown returns true if shutdown is in progress.
func (m *Manager) IsShuttingDown() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.shutdown
}

// Priority constants for common shutdown operations.
const (
	// PriorityStopAcceptingRequests stops accepting new requests first
	PriorityStopAcceptingRequests = 10

	// PriorityDrainRequests drains in-flight requests
	PriorityDrainRequests = 20

	// PriorityStopWorkers stops background workers
	PriorityStopWorkers = 30

	// PriorityCloseConnections closes connections to dependencies
	PriorityCloseConnections = 40

	// PriorityCleanup final cleanup operations
	PriorityCleanup = 50
)
