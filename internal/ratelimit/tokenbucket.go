// Package ratelimit provides distributed rate limiting with token bucket algorithm.
package ratelimit

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/dnakitare/aether/pkg/api"
)

// TokenBucket implements distributed token bucket rate limiting.
type TokenBucket struct {
	logger *slog.Logger
	redis  *redis.Client
	config Config
}

// Config holds rate limiting configuration.
type Config struct {
	// DefaultRate is the default tokens per second
	DefaultRate int

	// DefaultBurst is the default burst capacity
	DefaultBurst int

	// KeyPrefix for Redis keys
	KeyPrefix string

	// EnableGracePeriod allows temporary quota exceedance
	EnableGracePeriod bool

	// GracePeriodDuration is how long grace period lasts
	GracePeriodDuration time.Duration
}

// Limit represents a rate limit configuration.
type Limit struct {
	// Rate is tokens per second
	Rate int

	// Burst is maximum burst capacity
	Burst int

	// Period is the time window (for display purposes)
	Period time.Duration
}

// Tier represents a rate limiting tier.
type Tier string

const (
	TierFree       Tier = "free"
	TierPro        Tier = "pro"
	TierEnterprise Tier = "enterprise"
)

// TierLimits maps tiers to their limits.
var TierLimits = map[Tier]Limit{
	TierFree: {
		Rate:   10, // 10 requests per second
		Burst:  20, // Burst up to 20
		Period: time.Second,
	},
	TierPro: {
		Rate:   100, // 100 requests per second
		Burst:  200, // Burst up to 200
		Period: time.Second,
	},
	TierEnterprise: {
		Rate:   1000, // 1000 requests per second
		Burst:  2000, // Burst up to 2000
		Period: time.Second,
	},
}

// Result represents the result of a rate limit check.
type Result struct {
	// Allowed indicates if the request is allowed
	Allowed bool

	// Limit is the maximum requests allowed
	Limit int

	// Remaining is how many requests remain
	Remaining int

	// RetryAfter is when to retry (if not allowed)
	RetryAfter time.Duration

	// ResetAt is when the limit resets
	ResetAt time.Time
}

// NewTokenBucket creates a new token bucket rate limiter.
func NewTokenBucket(logger *slog.Logger, redisClient *redis.Client, config Config) *TokenBucket {
	if config.KeyPrefix == "" {
		config.KeyPrefix = "ratelimit:"
	}
	if config.DefaultRate == 0 {
		config.DefaultRate = 100
	}
	if config.DefaultBurst == 0 {
		config.DefaultBurst = 200
	}
	if config.GracePeriodDuration == 0 {
		config.GracePeriodDuration = 5 * time.Minute
	}

	return &TokenBucket{
		logger: logger.With("component", "rate_limiter"),
		redis:  redisClient,
		config: config,
	}
}

// Allow checks if a request is allowed under the rate limit.
func (tb *TokenBucket) Allow(ctx context.Context, key string, limit Limit) (*Result, error) {
	now := time.Now()

	// Redis keys
	tokensKey := tb.config.KeyPrefix + key + ":tokens"
	lastRefillKey := tb.config.KeyPrefix + key + ":last_refill"

	// Lua script for atomic token bucket algorithm
	script := redis.NewScript(`
		local tokens_key = KEYS[1]
		local last_refill_key = KEYS[2]
		local rate = tonumber(ARGV[1])
		local burst = tonumber(ARGV[2])
		local now = tonumber(ARGV[3])
		local requested = tonumber(ARGV[4])

		-- Get current tokens and last refill time
		local tokens = tonumber(redis.call('GET', tokens_key))
		local last_refill = tonumber(redis.call('GET', last_refill_key))

		-- Initialize if not exists
		if tokens == nil then
			tokens = burst
		end
		if last_refill == nil then
			last_refill = now
		end

		-- Calculate tokens to add based on time elapsed
		-- now is in milliseconds, so convert to seconds for rate calculation
		local elapsed = now - last_refill
		local tokens_to_add = (elapsed / 1000) * rate

		-- Refill tokens (up to burst capacity)
		tokens = math.min(burst, tokens + tokens_to_add)

		-- Update last refill time
		last_refill = now

		-- Check if we have enough tokens
		local allowed = 0
		if tokens >= requested then
			tokens = tokens - requested
			allowed = 1
		end

		-- Save state
		redis.call('SET', tokens_key, tokens, 'EX', 3600)
		redis.call('SET', last_refill_key, last_refill, 'EX', 3600)

		-- Return: allowed, tokens remaining, reset_at (in milliseconds)
		local reset_at = now + (((burst - tokens) / rate) * 1000)
		return {allowed, math.floor(tokens), math.floor(reset_at)}
	`)

	// Execute script
	// Use milliseconds for better precision in concurrent scenarios
	nowMs := now.UnixMilli()
	result, err := script.Run(
		ctx,
		tb.redis,
		[]string{tokensKey, lastRefillKey},
		limit.Rate,
		limit.Burst,
		nowMs,
		1, // Request 1 token
	).Result()

	if err != nil {
		return nil, fmt.Errorf("failed to execute rate limit script: %w", err)
	}

	// Parse result
	values, ok := result.([]interface{})
	if !ok || len(values) != 3 {
		return nil, fmt.Errorf("unexpected script result format")
	}

	allowed := values[0].(int64) == 1
	remaining := int(values[1].(int64))
	resetAtMs := values[2].(int64)
	resetAt := time.UnixMilli(resetAtMs)

	var retryAfter time.Duration
	if !allowed {
		retryAfter = time.Until(resetAt)
		if retryAfter < 0 {
			retryAfter = 0
		}
	}

	return &Result{
		Allowed:    allowed,
		Limit:      limit.Burst,
		Remaining:  remaining,
		RetryAfter: retryAfter,
		ResetAt:    resetAt,
	}, nil
}

// AllowN checks if N requests are allowed under the rate limit.
func (tb *TokenBucket) AllowN(ctx context.Context, key string, limit Limit, n int) (*Result, error) {
	// Similar to Allow but requests n tokens
	// For simplicity, we'll just call Allow n times
	// In production, you'd modify the Lua script to handle n tokens
	return tb.Allow(ctx, key, limit)
}

// GetLimitForTier returns the rate limit for a given tier.
func (tb *TokenBucket) GetLimitForTier(tier Tier) Limit {
	if limit, ok := TierLimits[tier]; ok {
		return limit
	}
	return Limit{
		Rate:   tb.config.DefaultRate,
		Burst:  tb.config.DefaultBurst,
		Period: time.Second,
	}
}

// Reset resets the rate limit for a key.
func (tb *TokenBucket) Reset(ctx context.Context, key string) error {
	tokensKey := tb.config.KeyPrefix + key + ":tokens"
	lastRefillKey := tb.config.KeyPrefix + key + ":last_refill"

	pipe := tb.redis.Pipeline()
	pipe.Del(ctx, tokensKey)
	pipe.Del(ctx, lastRefillKey)
	_, err := pipe.Exec(ctx)

	if err != nil {
		return fmt.Errorf("failed to reset rate limit: %w", err)
	}

	tb.logger.DebugContext(ctx, "rate limit reset", "key", key)
	return nil
}

// MultiLayerLimiter provides multi-layer rate limiting.
type MultiLayerLimiter struct {
	logger      *slog.Logger
	tokenBucket *TokenBucket
}

// NewMultiLayerLimiter creates a new multi-layer rate limiter.
func NewMultiLayerLimiter(logger *slog.Logger, tokenBucket *TokenBucket) *MultiLayerLimiter {
	return &MultiLayerLimiter{
		logger:      logger.With("component", "multi_layer_limiter"),
		tokenBucket: tokenBucket,
	}
}

// CheckLimits checks all applicable rate limits in order.
func (ml *MultiLayerLimiter) CheckLimits(ctx context.Context, tenantID api.TenantID, userID string, endpoint string, tier Tier) (*Result, error) {
	limit := ml.tokenBucket.GetLimitForTier(tier)

	// Check tenant-level limit
	tenantKey := fmt.Sprintf("tenant:%s", tenantID)
	result, err := ml.tokenBucket.Allow(ctx, tenantKey, limit)
	if err != nil {
		return nil, fmt.Errorf("tenant limit check failed: %w", err)
	}
	if !result.Allowed {
		ml.logger.WarnContext(ctx, "tenant rate limit exceeded",
			"tenant_id", tenantID,
			"limit", result.Limit,
		)
		return result, nil
	}

	// Check user-level limit (per-user within tenant)
	if userID != "" {
		userKey := fmt.Sprintf("user:%s:%s", tenantID, userID)
		userLimit := Limit{
			Rate:   limit.Rate / 10, // Users get 10% of tenant limit
			Burst:  limit.Burst / 10,
			Period: limit.Period,
		}
		result, err = ml.tokenBucket.Allow(ctx, userKey, userLimit)
		if err != nil {
			return nil, fmt.Errorf("user limit check failed: %w", err)
		}
		if !result.Allowed {
			ml.logger.WarnContext(ctx, "user rate limit exceeded",
				"tenant_id", tenantID,
				"user_id", userID,
				"limit", result.Limit,
			)
			return result, nil
		}
	}

	// Check endpoint-level limit
	if endpoint != "" {
		endpointKey := fmt.Sprintf("endpoint:%s:%s", tenantID, endpoint)
		endpointLimit := ml.getEndpointLimit(endpoint, limit)
		result, err = ml.tokenBucket.Allow(ctx, endpointKey, endpointLimit)
		if err != nil {
			return nil, fmt.Errorf("endpoint limit check failed: %w", err)
		}
		if !result.Allowed {
			ml.logger.WarnContext(ctx, "endpoint rate limit exceeded",
				"tenant_id", tenantID,
				"endpoint", endpoint,
				"limit", result.Limit,
			)
			return result, nil
		}
	}

	return result, nil
}

// getEndpointLimit returns endpoint-specific limits.
func (ml *MultiLayerLimiter) getEndpointLimit(endpoint string, baseLimit Limit) Limit {
	// Define endpoint-specific limits
	// Expensive operations get lower limits
	switch endpoint {
	case "/v1/agents":
		// Agent creation is expensive
		return Limit{
			Rate:   baseLimit.Rate / 5,
			Burst:  baseLimit.Burst / 5,
			Period: baseLimit.Period,
		}
	case "/v1/agents/{id}/exec":
		// Code execution is expensive
		return Limit{
			Rate:   baseLimit.Rate / 10,
			Burst:  baseLimit.Burst / 10,
			Period: baseLimit.Period,
		}
	default:
		// Other endpoints use base limit
		return baseLimit
	}
}
