package retry

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDo_Success(t *testing.T) {
	ctx := context.Background()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	config := DefaultConfig()
	callCount := 0

	err := Do(ctx, logger, config, func(ctx context.Context) error {
		callCount++
		return nil
	})

	require.NoError(t, err)
	assert.Equal(t, 1, callCount, "Should succeed on first try")
}

func TestDo_RetryAndSucceed(t *testing.T) {
	ctx := context.Background()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	config := DefaultConfig()
	config.InitialDelay = 1 * time.Millisecond
	callCount := 0

	err := Do(ctx, logger, config, func(ctx context.Context) error {
		callCount++
		if callCount < 3 {
			return errors.New("temporary error")
		}
		return nil
	})

	require.NoError(t, err)
	assert.Equal(t, 3, callCount, "Should succeed on third try")
}

func TestDo_MaxRetriesExceeded(t *testing.T) {
	ctx := context.Background()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	config := DefaultConfig()
	config.MaxAttempts = 2
	config.InitialDelay = 1 * time.Millisecond
	callCount := 0

	err := Do(ctx, logger, config, func(ctx context.Context) error {
		callCount++
		return errors.New("persistent error")
	})

	require.Error(t, err)
	assert.Equal(t, 3, callCount, "Should try initial + 2 retries")
	assert.Contains(t, err.Error(), "after 3 attempts")
}

func TestDo_NonRetryableError(t *testing.T) {
	ctx := context.Background()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	config := DefaultConfig()
	config.RetryableFunc = func(err error) bool {
		return false // No errors are retryable
	}
	callCount := 0

	err := Do(ctx, logger, config, func(ctx context.Context) error {
		callCount++
		return errors.New("non-retryable error")
	})

	require.Error(t, err)
	assert.Equal(t, 1, callCount, "Should not retry non-retryable errors")
}

func TestDo_ContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	config := DefaultConfig()
	config.InitialDelay = 50 * time.Millisecond
	callCount := 0

	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	err := Do(ctx, logger, config, func(ctx context.Context) error {
		callCount++
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			return errors.New("error")
		}
	})

	require.Error(t, err)
}

func TestDoWithValue_Success(t *testing.T) {
	ctx := context.Background()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	config := DefaultConfig()

	result, err := DoWithValue(ctx, logger, config, func(ctx context.Context) (string, error) {
		return "success", nil
	})

	require.NoError(t, err)
	assert.Equal(t, "success", result)
}

func TestDoWithValue_RetryAndSucceed(t *testing.T) {
	ctx := context.Background()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	config := DefaultConfig()
	config.InitialDelay = 1 * time.Millisecond
	callCount := 0

	result, err := DoWithValue(ctx, logger, config, func(ctx context.Context) (int, error) {
		callCount++
		if callCount < 2 {
			return 0, errors.New("error")
		}
		return 42, nil
	})

	require.NoError(t, err)
	assert.Equal(t, 42, result)
	assert.Equal(t, 2, callCount)
}

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()
	assert.Equal(t, 3, config.MaxAttempts)
	assert.Equal(t, 100*time.Millisecond, config.InitialDelay)
	assert.Equal(t, 10*time.Second, config.MaxDelay)
	assert.Equal(t, 2.0, config.Multiplier)
	assert.True(t, config.Jitter)
	assert.NotNil(t, config.RetryableFunc)
}

func TestDefaultRetryable(t *testing.T) {
	assert.False(t, DefaultRetryable(nil), "nil error should not be retryable")
	assert.True(t, DefaultRetryable(errors.New("error")), "non-nil error should be retryable")
}
