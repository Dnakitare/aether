package retry

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

// CircuitState represents the state of the circuit breaker.
type CircuitState int32

const (
	// StateClosed allows all requests through
	StateClosed CircuitState = iota

	// StateOpen rejects all requests
	StateOpen

	// StateHalfOpen allows limited requests to test if service recovered
	StateHalfOpen
)

func (s CircuitState) String() string {
	switch s {
	case StateClosed:
		return "closed"
	case StateOpen:
		return "open"
	case StateHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

// CircuitBreakerConfig configures circuit breaker behavior.
type CircuitBreakerConfig struct {
	// MaxFailures is the number of failures before opening the circuit
	MaxFailures int

	// Timeout is how long to wait before transitioning from Open to Half-Open
	Timeout time.Duration

	// MaxRequests is the number of requests allowed in Half-Open state
	MaxRequests int

	// SuccessThreshold is the number of successful requests needed in Half-Open
	// to transition back to Closed state
	SuccessThreshold int

	// OnStateChange is called when the circuit state changes
	OnStateChange func(from, to CircuitState)
}

// DefaultCircuitBreakerConfig returns sensible defaults.
func DefaultCircuitBreakerConfig() CircuitBreakerConfig {
	return CircuitBreakerConfig{
		MaxFailures:      5,
		Timeout:          30 * time.Second,
		MaxRequests:      3,
		SuccessThreshold: 2,
	}
}

// CircuitBreaker implements the circuit breaker pattern.
type CircuitBreaker struct {
	config    CircuitBreakerConfig
	logger    *slog.Logger
	state     atomic.Int32 // CircuitState
	failures  atomic.Int32
	successes atomic.Int32
	requests  atomic.Int32 // For half-open state
	lastError atomic.Value // Last error that triggered opening
	mu        sync.Mutex
	openedAt  time.Time
}

// NewCircuitBreaker creates a new circuit breaker.
func NewCircuitBreaker(logger *slog.Logger, config CircuitBreakerConfig) *CircuitBreaker {
	// Validate config values fit in int32 to prevent overflow
	if config.MaxFailures < 0 || config.MaxFailures > 1000000 {
		config.MaxFailures = 5 // Use safe default
	}
	if config.MaxRequests < 0 || config.MaxRequests > 1000000 {
		config.MaxRequests = 3 // Use safe default
	}
	if config.SuccessThreshold < 0 || config.SuccessThreshold > 1000000 {
		config.SuccessThreshold = 2 // Use safe default
	}

	cb := &CircuitBreaker{
		config: config,
		logger: logger.With("component", "circuit_breaker"),
	}
	cb.state.Store(int32(StateClosed))
	return cb
}

// Execute runs the operation through the circuit breaker.
func (cb *CircuitBreaker) Execute(ctx context.Context, op Operation) error {
	// Check if circuit is open
	if err := cb.beforeRequest(ctx); err != nil {
		return err
	}

	// Execute operation
	err := op(ctx)

	// Record result
	cb.afterRequest(err)

	return err
}

// ExecuteWithValue runs an operation that returns a value through the circuit breaker.
// Note: This uses interface{} for compatibility with Go 1.21
func (cb *CircuitBreaker) ExecuteWithValue(ctx context.Context, op func(ctx context.Context) (interface{}, error)) (interface{}, error) {
	// Check if circuit is open
	if err := cb.beforeRequest(ctx); err != nil {
		return nil, err
	}

	// Execute operation
	val, err := op(ctx)

	// Record result
	cb.afterRequest(err)

	return val, err
}

// beforeRequest checks if the request should be allowed.
func (cb *CircuitBreaker) beforeRequest(ctx context.Context) error {
	state := CircuitState(cb.state.Load())

	switch state {
	case StateClosed:
		// Allow request
		return nil

	case StateOpen:
		// Check if timeout has elapsed
		cb.mu.Lock()
		if time.Since(cb.openedAt) >= cb.config.Timeout {
			// Transition to half-open
			cb.setState(StateHalfOpen)
			cb.requests.Store(0)
			cb.successes.Store(0)
			cb.mu.Unlock()
			return nil
		}
		cb.mu.Unlock()

		// Circuit is still open, reject request
		lastErr := cb.lastError.Load()
		if lastErr != nil {
			return fmt.Errorf("circuit breaker is open (last error: %v)", lastErr)
		}
		return fmt.Errorf("circuit breaker is open")

	case StateHalfOpen:
		// Check if we've reached max requests in half-open state
		// #nosec G115 - MaxRequests validated in NewCircuitBreaker to be <= 1000000
		if cb.requests.Load() >= int32(cb.config.MaxRequests) {
			return fmt.Errorf("circuit breaker is half-open and at capacity")
		}
		cb.requests.Add(1)
		return nil

	default:
		return fmt.Errorf("unknown circuit breaker state: %v", state)
	}
}

// afterRequest records the result of a request.
func (cb *CircuitBreaker) afterRequest(err error) {
	state := CircuitState(cb.state.Load())

	if err != nil {
		// Request failed
		cb.onFailure(err)
	} else {
		// Request succeeded
		cb.onSuccess()
	}

	// Check if we need to transition states
	switch state {
	case StateClosed:
		// #nosec G115 - MaxFailures validated in NewCircuitBreaker to be <= 1000000
		if cb.failures.Load() >= int32(cb.config.MaxFailures) {
			cb.mu.Lock()
			cb.openedAt = time.Now()
			cb.mu.Unlock()
			cb.setState(StateOpen)
		}

	case StateHalfOpen:
		if err != nil {
			// Failure in half-open state reopens the circuit
			cb.mu.Lock()
			cb.openedAt = time.Now()
			cb.mu.Unlock()
			cb.setState(StateOpen)
			// #nosec G115 - SuccessThreshold validated in NewCircuitBreaker
		} else if cb.successes.Load() >= int32(cb.config.SuccessThreshold) {
			// Enough successes to close the circuit
			cb.setState(StateClosed)
		}
	}
}

// onSuccess records a successful request.
func (cb *CircuitBreaker) onSuccess() {
	state := CircuitState(cb.state.Load())

	switch state {
	case StateClosed:
		// Reset failure count on success
		cb.failures.Store(0)

	case StateHalfOpen:
		// Increment success count
		cb.successes.Add(1)
	}
}

// onFailure records a failed request.
func (cb *CircuitBreaker) onFailure(err error) {
	cb.lastError.Store(err)

	state := CircuitState(cb.state.Load())

	switch state {
	case StateClosed:
		// Increment failure count
		cb.failures.Add(1)

	case StateHalfOpen:
		// Any failure in half-open resets the circuit
		// #nosec G115 - MaxFailures validated in NewCircuitBreaker to be <= 1000000
		cb.failures.Store(int32(cb.config.MaxFailures))
	}
}

// setState transitions the circuit breaker to a new state.
func (cb *CircuitBreaker) setState(newState CircuitState) {
	oldState := CircuitState(cb.state.Swap(int32(newState)))

	if oldState != newState {
		cb.logger.Info("circuit breaker state changed",
			"from", oldState.String(),
			"to", newState.String(),
		)

		// Reset counters on state change
		switch newState {
		case StateClosed:
			cb.failures.Store(0)
			cb.successes.Store(0)
			cb.requests.Store(0)

		case StateOpen:
			cb.successes.Store(0)
			cb.requests.Store(0)

		case StateHalfOpen:
			cb.requests.Store(0)
			cb.successes.Store(0)
		}

		// Call state change callback
		if cb.config.OnStateChange != nil {
			cb.config.OnStateChange(oldState, newState)
		}
	}
}

// State returns the current circuit breaker state.
func (cb *CircuitBreaker) State() CircuitState {
	return CircuitState(cb.state.Load())
}

// Failures returns the current failure count.
func (cb *CircuitBreaker) Failures() int {
	return int(cb.failures.Load())
}

// Reset resets the circuit breaker to closed state.
func (cb *CircuitBreaker) Reset() {
	cb.setState(StateClosed)
	cb.logger.Info("circuit breaker manually reset")
}
