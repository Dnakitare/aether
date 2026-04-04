package retry

import (
	"context"
	cryptorand "crypto/rand"
	"encoding/binary"
	"fmt"
	"log/slog"
	"math"
	"time"
)

// Config configures retry behavior.
type Config struct {
	// MaxAttempts is the maximum number of retry attempts (0 = no retries)
	MaxAttempts int

	// InitialDelay is the initial delay between retries
	InitialDelay time.Duration

	// MaxDelay is the maximum delay between retries
	MaxDelay time.Duration

	// Multiplier is the backoff multiplier (e.g., 2.0 for exponential)
	Multiplier float64

	// Jitter adds randomness to prevent thundering herd
	Jitter bool

	// RetryableFunc determines if an error is retryable
	RetryableFunc func(error) bool
}

// DefaultConfig returns sensible default retry configuration.
func DefaultConfig() Config {
	return Config{
		MaxAttempts:   3,
		InitialDelay:  100 * time.Millisecond,
		MaxDelay:      10 * time.Second,
		Multiplier:    2.0,
		Jitter:        true,
		RetryableFunc: DefaultRetryable,
	}
}

// DefaultRetryable is the default function to determine if an error is retryable.
// By default, all errors are retryable.
func DefaultRetryable(err error) bool {
	return err != nil
}

// Operation is a function that can be retried.
type Operation func(ctx context.Context) error

// Do executes the operation with retries according to the config.
func Do(ctx context.Context, logger *slog.Logger, config Config, op Operation) error {
	var lastErr error
	delay := config.InitialDelay

	for attempt := 0; attempt <= config.MaxAttempts; attempt++ {
		// Execute operation
		err := op(ctx)
		if err == nil {
			if attempt > 0 {
				logger.InfoContext(ctx, "operation succeeded after retry",
					"attempt", attempt+1,
					"total_attempts", config.MaxAttempts+1,
				)
			}
			return nil
		}

		lastErr = err

		// Check if error is retryable
		if !config.RetryableFunc(err) {
			logger.WarnContext(ctx, "error not retryable",
				"error", err,
				"attempt", attempt+1,
			)
			return err
		}

		// Check if we've exhausted retries
		if attempt == config.MaxAttempts {
			logger.ErrorContext(ctx, "operation failed after max retries",
				"error", err,
				"attempts", attempt+1,
			)
			return fmt.Errorf("operation failed after %d attempts: %w", attempt+1, err)
		}

		// Calculate next delay with exponential backoff
		nextDelay := time.Duration(float64(delay) * config.Multiplier)
		if nextDelay > config.MaxDelay {
			nextDelay = config.MaxDelay
		}

		// Add jitter if enabled
		if config.Jitter {
			jitter := time.Duration(float64(nextDelay) * 0.1) // 10% jitter
			nextDelay += time.Duration(float64(jitter) * (2*randomFloat() - 1))
		}

		logger.WarnContext(ctx, "operation failed, retrying",
			"error", err,
			"attempt", attempt+1,
			"next_delay", nextDelay,
		)

		// Wait before retry
		select {
		case <-ctx.Done():
			return fmt.Errorf("context cancelled during retry: %w", ctx.Err())
		case <-time.After(nextDelay):
			delay = nextDelay
		}
	}

	return lastErr
}

// DoWithValue executes an operation that returns a value, with retries.
func DoWithValue[T any](ctx context.Context, logger *slog.Logger, config Config, op func(ctx context.Context) (T, error)) (T, error) {
	var result T
	var lastErr error
	delay := config.InitialDelay

	for attempt := 0; attempt <= config.MaxAttempts; attempt++ {
		// Execute operation
		val, err := op(ctx)
		if err == nil {
			if attempt > 0 {
				logger.InfoContext(ctx, "operation succeeded after retry",
					"attempt", attempt+1,
					"total_attempts", config.MaxAttempts+1,
				)
			}
			return val, nil
		}

		lastErr = err

		// Check if error is retryable
		if !config.RetryableFunc(err) {
			logger.WarnContext(ctx, "error not retryable",
				"error", err,
				"attempt", attempt+1,
			)
			return result, err
		}

		// Check if we've exhausted retries
		if attempt == config.MaxAttempts {
			logger.ErrorContext(ctx, "operation failed after max retries",
				"error", err,
				"attempts", attempt+1,
			)
			return result, fmt.Errorf("operation failed after %d attempts: %w", attempt+1, err)
		}

		// Calculate next delay with exponential backoff
		nextDelay := time.Duration(float64(delay) * config.Multiplier)
		if nextDelay > config.MaxDelay {
			nextDelay = config.MaxDelay
		}

		// Add jitter if enabled
		if config.Jitter {
			jitter := time.Duration(float64(nextDelay) * 0.1) // 10% jitter
			nextDelay += time.Duration(float64(jitter) * (2*randomFloat() - 1))
		}

		logger.WarnContext(ctx, "operation failed, retrying",
			"error", err,
			"attempt", attempt+1,
			"next_delay", nextDelay,
		)

		// Wait before retry
		select {
		case <-ctx.Done():
			return result, fmt.Errorf("context cancelled during retry: %w", ctx.Err())
		case <-time.After(nextDelay):
			delay = nextDelay
		}
	}

	return result, lastErr
}

// randomFloat returns a cryptographically random float in [0.0, 1.0).
func randomFloat() float64 {
	var b [8]byte
	if _, err := cryptorand.Read(b[:]); err != nil {
		// Fall back to time-based jitter on the extremely unlikely read failure.
		return math.Abs(math.Sin(float64(time.Now().UnixNano())))
	}
	// Use top 53 bits for a uniform float64 in [0, 1).
	return float64(binary.BigEndian.Uint64(b[:])>>11) / (1 << 53)
}
