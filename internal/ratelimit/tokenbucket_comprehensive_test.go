package ratelimit_test

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/aether-runtime/aether/internal/ratelimit"
	"github.com/aether-runtime/aether/pkg/api"
)

// ============================================================================
// Test Helpers
// ============================================================================

func setupTestTokenBucket(t *testing.T) (*ratelimit.TokenBucket, *miniredis.Miniredis, func()) {
	t.Helper()

	// Create miniredis server (in-memory Redis mock)
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("Failed to create miniredis: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	redisClient := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})

	config := ratelimit.Config{
		DefaultRate:         100,
		DefaultBurst:        200,
		KeyPrefix:           "test:",
		EnableGracePeriod:   false,
		GracePeriodDuration: 5 * time.Minute,
	}

	tb := ratelimit.NewTokenBucket(logger, redisClient, config)

	cleanup := func() {
		redisClient.Close()
		mr.Close()
	}

	return tb, mr, cleanup
}

// ============================================================================
// Configuration Tests
// ============================================================================

func TestNewTokenBucket(t *testing.T) {
	t.Run("default_configuration", func(t *testing.T) {
		logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

		mr, err := miniredis.Run()
		if err != nil {
			t.Fatalf("Failed to create miniredis: %v", err)
		}
		defer mr.Close()

		redisClient := redis.NewClient(&redis.Options{Addr: mr.Addr()})
		defer redisClient.Close()

		// Empty config should get defaults
		tb := ratelimit.NewTokenBucket(logger, redisClient, ratelimit.Config{})

		// Verify defaults are applied via GetLimitForTier
		limit := tb.GetLimitForTier("unknown-tier")
		if limit.Rate != 100 {
			t.Errorf("Default rate = %d, want 100", limit.Rate)
		}
		if limit.Burst != 200 {
			t.Errorf("Default burst = %d, want 200", limit.Burst)
		}
	})

	t.Run("custom_configuration", func(t *testing.T) {
		logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

		mr, err := miniredis.Run()
		if err != nil {
			t.Fatalf("Failed to create miniredis: %v", err)
		}
		defer mr.Close()

		redisClient := redis.NewClient(&redis.Options{Addr: mr.Addr()})
		defer redisClient.Close()

		config := ratelimit.Config{
			DefaultRate:         50,
			DefaultBurst:        100,
			KeyPrefix:           "custom:",
			EnableGracePeriod:   true,
			GracePeriodDuration: 10 * time.Minute,
		}

		tb := ratelimit.NewTokenBucket(logger, redisClient, config)

		limit := tb.GetLimitForTier("unknown-tier")
		if limit.Rate != 50 {
			t.Errorf("Custom rate = %d, want 50", limit.Rate)
		}
		if limit.Burst != 100 {
			t.Errorf("Custom burst = %d, want 100", limit.Burst)
		}
	})
}

// ============================================================================
// Tier Limits Tests
// ============================================================================

func TestGetLimitForTier(t *testing.T) {
	tb, _, cleanup := setupTestTokenBucket(t)
	defer cleanup()

	tests := []struct {
		tier         ratelimit.Tier
		expectedRate int
	}{
		{ratelimit.TierFree, 10},
		{ratelimit.TierPro, 100},
		{ratelimit.TierEnterprise, 1000},
		{"unknown-tier", 100}, // Should use default
	}

	for _, tt := range tests {
		t.Run(string(tt.tier), func(t *testing.T) {
			limit := tb.GetLimitForTier(tt.tier)

			if limit.Rate != tt.expectedRate {
				t.Errorf("Rate = %d, want %d", limit.Rate, tt.expectedRate)
			}

			if limit.Burst != tt.expectedRate*2 {
				t.Errorf("Burst = %d, want %d", limit.Burst, tt.expectedRate*2)
			}

			if limit.Period != time.Second {
				t.Errorf("Period = %v, want 1s", limit.Period)
			}
		})
	}
}

// ============================================================================
// Token Bucket Algorithm Tests
// ============================================================================

func TestTokenBucketBasicAllow(t *testing.T) {
	t.Run("first_request_allowed", func(t *testing.T) {
		tb, mr, cleanup := setupTestTokenBucket(t)
		defer cleanup()

		ctx := context.Background()
		limit := ratelimit.Limit{Rate: 10, Burst: 20, Period: time.Second}

		result, err := tb.Allow(ctx, "user:1", limit)
		if err != nil {
			t.Fatalf("Allow failed: %v", err)
		}

		if !result.Allowed {
			t.Error("First request should be allowed")
		}

		if result.Limit != 20 {
			t.Errorf("Limit = %d, want 20", result.Limit)
		}

		// Should have 19 tokens remaining (burst 20 - 1 consumed)
		if result.Remaining != 19 {
			t.Errorf("Remaining = %d, want 19", result.Remaining)
		}

		// Verify Redis state
		tokensKey := "test:user:1:tokens"
		tokens, _ := mr.Get(tokensKey)
		if tokens != "19" {
			t.Errorf("Redis tokens = %s, want 19", tokens)
		}
	})

	t.Run("burst_consumption", func(t *testing.T) {
		tb, _, cleanup := setupTestTokenBucket(t)
		defer cleanup()

		ctx := context.Background()
		limit := ratelimit.Limit{Rate: 1, Burst: 5, Period: time.Second}

		// Consume all burst tokens
		for i := 0; i < 5; i++ {
			result, err := tb.Allow(ctx, "user:burst", limit)
			if err != nil {
				t.Fatalf("Allow %d failed: %v", i, err)
			}
			if !result.Allowed {
				t.Errorf("Request %d should be allowed", i)
			}
		}

		// 6th request should be denied
		result, err := tb.Allow(ctx, "user:burst", limit)
		if err != nil {
			t.Fatalf("Allow failed: %v", err)
		}

		if result.Allowed {
			t.Error("Request beyond burst should be denied")
		}

		if result.Remaining != 0 {
			t.Errorf("Remaining = %d, want 0", result.Remaining)
		}

		if result.RetryAfter <= 0 {
			t.Error("RetryAfter should be set when denied")
		}
	})

	t.Run("token_refill", func(t *testing.T) {
		tb, mr, cleanup := setupTestTokenBucket(t)
		defer cleanup()

		ctx := context.Background()
		limit := ratelimit.Limit{Rate: 10, Burst: 20, Period: time.Second}

		// First request (consumes 1 token, leaving 19)
		result1, err := tb.Allow(ctx, "user:refill", limit)
		if err != nil {
			t.Fatalf("Allow failed: %v", err)
		}
		if !result1.Allowed {
			t.Error("First request should be allowed")
		}
		if result1.Remaining != 19 {
			t.Errorf("After first request, remaining = %d, want 19", result1.Remaining)
		}

		// Fast-forward time in miniredis by 2 seconds
		// With rate=10 tokens/sec, 2 seconds = 20 tokens refill
		mr.FastForward(2 * time.Second)

		// Second request after refill (bucket refills to burst capacity 20, then consumes 1)
		result2, err := tb.Allow(ctx, "user:refill", limit)
		if err != nil {
			t.Fatalf("Allow failed: %v", err)
		}

		if !result2.Allowed {
			t.Error("Request after refill should be allowed")
		}

		// After refill to 20 and consuming 1 token, should have 19
		if result2.Remaining < 18 || result2.Remaining > 19 {
			t.Errorf("Remaining after refill = %d, want 18-19 (refilled to burst minus consumed)", result2.Remaining)
		}
	})
}

func TestTokenBucketReset(t *testing.T) {
	t.Run("reset_clears_state", func(t *testing.T) {
		tb, mr, cleanup := setupTestTokenBucket(t)
		defer cleanup()

		ctx := context.Background()
		limit := ratelimit.Limit{Rate: 1, Burst: 5, Period: time.Second}

		// Consume all tokens
		for i := 0; i < 5; i++ {
			tb.Allow(ctx, "user:reset", limit)
		}

		// Verify depleted
		result, _ := tb.Allow(ctx, "user:reset", limit)
		if result.Allowed {
			t.Error("Should be rate limited before reset")
		}

		// Reset
		err := tb.Reset(ctx, "user:reset")
		if err != nil {
			t.Fatalf("Reset failed: %v", err)
		}

		// Verify Redis keys are deleted
		tokensKey := "test:user:reset:tokens"
		if mr.Exists(tokensKey) {
			t.Error("Tokens key should be deleted after reset")
		}

		// New request should be allowed (fresh bucket)
		result, err = tb.Allow(ctx, "user:reset", limit)
		if err != nil {
			t.Fatalf("Allow after reset failed: %v", err)
		}

		if !result.Allowed {
			t.Error("Request should be allowed after reset")
		}
	})
}

// ============================================================================
// Concurrent Access Tests
// ============================================================================

func TestTokenBucketConcurrency(t *testing.T) {
	t.Run("concurrent_requests", func(t *testing.T) {
		tb, _, cleanup := setupTestTokenBucket(t)
		defer cleanup()

		ctx := context.Background()
		limit := ratelimit.Limit{Rate: 100, Burst: 200, Period: time.Second}

		const numGoroutines = 50
		const requestsPerGoroutine = 10

		var wg sync.WaitGroup
		var allowedCount sync.Map
		var deniedCount sync.Map

		wg.Add(numGoroutines)

		for g := 0; g < numGoroutines; g++ {
			go func(goroutineID int) {
				defer wg.Done()

				allowed := 0
				denied := 0

				for i := 0; i < requestsPerGoroutine; i++ {
					result, err := tb.Allow(ctx, "user:concurrent", limit)
					if err != nil {
						t.Errorf("Allow failed: %v", err)
						return
					}

					if result.Allowed {
						allowed++
					} else {
						denied++
					}
				}

				allowedCount.Store(goroutineID, allowed)
				deniedCount.Store(goroutineID, denied)
			}(g)
		}

		wg.Wait()

		// Calculate totals
		totalAllowed := 0
		totalDenied := 0

		allowedCount.Range(func(key, value interface{}) bool {
			totalAllowed += value.(int)
			return true
		})

		deniedCount.Range(func(key, value interface{}) bool {
			totalDenied += value.(int)
			return true
		})

		totalRequests := numGoroutines * requestsPerGoroutine

		if totalAllowed+totalDenied != totalRequests {
			t.Errorf("Total processed = %d, want %d", totalAllowed+totalDenied, totalRequests)
		}

		// Should allow up to burst capacity (200)
		// Allow tolerance for concurrent edge cases and token refill during test execution
		// With 50 goroutines taking ~200ms to complete, tokens refill at 100/sec = 20 tokens
		tolerance := 25
		if totalAllowed > limit.Burst+tolerance {
			t.Errorf("Allowed %d requests, exceeds burst limit %d (tolerance: %d)", totalAllowed, limit.Burst, tolerance)
		}

		// Should have denied some requests (500 total, 200 burst)
		if totalDenied == 0 {
			t.Error("Expected some requests to be denied in concurrent scenario")
		}
	})

	t.Run("concurrent_different_keys", func(t *testing.T) {
		tb, _, cleanup := setupTestTokenBucket(t)
		defer cleanup()

		ctx := context.Background()
		limit := ratelimit.Limit{Rate: 5, Burst: 10, Period: time.Second}

		const numUsers = 10
		var wg sync.WaitGroup
		wg.Add(numUsers)

		for u := 0; u < numUsers; u++ {
			go func(userID int) {
				defer wg.Done()

				key := fmt.Sprintf("user:%d", userID)

				// Each user should get their own bucket
				result, err := tb.Allow(ctx, key, limit)
				if err != nil {
					t.Errorf("Allow failed for user %d: %v", userID, err)
					return
				}

				if !result.Allowed {
					t.Errorf("User %d should be allowed (separate bucket)", userID)
				}
			}(u)
		}

		wg.Wait()
	})
}

// ============================================================================
// Multi-Layer Limiter Tests
// ============================================================================

func TestMultiLayerLimiterCheckLimits(t *testing.T) {
	t.Run("tenant_level_limit", func(t *testing.T) {
		tb, _, cleanup := setupTestTokenBucket(t)
		defer cleanup()

		logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
		ml := ratelimit.NewMultiLayerLimiter(logger, tb)

		ctx := context.Background()
		tenantID := api.TenantID("tenant-1")

		result, err := ml.CheckLimits(ctx, tenantID, "", "", ratelimit.TierFree)
		if err != nil {
			t.Fatalf("CheckLimits failed: %v", err)
		}

		if !result.Allowed {
			t.Error("First tenant request should be allowed")
		}
	})

	t.Run("user_level_limit", func(t *testing.T) {
		tb, _, cleanup := setupTestTokenBucket(t)
		defer cleanup()

		logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
		ml := ratelimit.NewMultiLayerLimiter(logger, tb)

		ctx := context.Background()
		tenantID := api.TenantID("tenant-1")
		userID := "user-1"

		result, err := ml.CheckLimits(ctx, tenantID, userID, "", ratelimit.TierFree)
		if err != nil {
			t.Fatalf("CheckLimits failed: %v", err)
		}

		if !result.Allowed {
			t.Error("First user request should be allowed")
		}
	})

	t.Run("endpoint_level_limit", func(t *testing.T) {
		tb, _, cleanup := setupTestTokenBucket(t)
		defer cleanup()

		logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
		ml := ratelimit.NewMultiLayerLimiter(logger, tb)

		ctx := context.Background()
		tenantID := api.TenantID("tenant-1")
		endpoint := "/v1/agents"

		result, err := ml.CheckLimits(ctx, tenantID, "", endpoint, ratelimit.TierFree)
		if err != nil {
			t.Fatalf("CheckLimits failed: %v", err)
		}

		if !result.Allowed {
			t.Error("First endpoint request should be allowed")
		}
	})

	t.Run("tenant_limit_exceeded", func(t *testing.T) {
		tb, _, cleanup := setupTestTokenBucket(t)
		defer cleanup()

		logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
		ml := ratelimit.NewMultiLayerLimiter(logger, tb)

		ctx := context.Background()
		tenantID := api.TenantID("tenant-exhausted")

		// Free tier has 10 req/s, burst 20
		// Exhaust the tenant bucket
		for i := 0; i < 20; i++ {
			ml.CheckLimits(ctx, tenantID, "", "", ratelimit.TierFree)
		}

		// Next request should be denied
		result, err := ml.CheckLimits(ctx, tenantID, "", "", ratelimit.TierFree)
		if err != nil {
			t.Fatalf("CheckLimits failed: %v", err)
		}

		if result.Allowed {
			t.Error("Request should be denied after exhausting tenant limit")
		}
	})

	t.Run("user_limit_exceeded_tenant_ok", func(t *testing.T) {
		tb, _, cleanup := setupTestTokenBucket(t)
		defer cleanup()

		logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
		ml := ratelimit.NewMultiLayerLimiter(logger, tb)

		ctx := context.Background()
		tenantID := api.TenantID("tenant-user-limit")
		userID := "greedy-user"

		// User gets 10% of tenant limit (Free tier: 10 req/s -> 1 req/s user, burst 2)
		// Exhaust user bucket
		for i := 0; i < 2; i++ {
			result, _ := ml.CheckLimits(ctx, tenantID, userID, "", ratelimit.TierFree)
			if !result.Allowed {
				t.Errorf("Request %d should be allowed", i)
			}
		}

		// Next request should be denied (user limit exceeded)
		result, err := ml.CheckLimits(ctx, tenantID, userID, "", ratelimit.TierFree)
		if err != nil {
			t.Fatalf("CheckLimits failed: %v", err)
		}

		if result.Allowed {
			t.Error("Request should be denied after exhausting user limit")
		}

		// Different user should still work (tenant limit not exceeded)
		result2, err := ml.CheckLimits(ctx, tenantID, "different-user", "", ratelimit.TierFree)
		if err != nil {
			t.Fatalf("CheckLimits failed: %v", err)
		}

		if !result2.Allowed {
			t.Error("Different user should be allowed (tenant limit not exceeded)")
		}
	})

	t.Run("expensive_endpoint_lower_limit", func(t *testing.T) {
		tb, _, cleanup := setupTestTokenBucket(t)
		defer cleanup()

		logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
		ml := ratelimit.NewMultiLayerLimiter(logger, tb)

		ctx := context.Background()
		tenantID := api.TenantID("tenant-endpoint")

		// /v1/agents has 1/5 of base limit (Free: 10 req/s -> 2 req/s, burst 4)
		for i := 0; i < 4; i++ {
			result, _ := ml.CheckLimits(ctx, tenantID, "", "/v1/agents", ratelimit.TierFree)
			if !result.Allowed {
				t.Errorf("Request %d should be allowed", i)
			}
		}

		// Next request should be denied
		result, err := ml.CheckLimits(ctx, tenantID, "", "/v1/agents", ratelimit.TierFree)
		if err != nil {
			t.Fatalf("CheckLimits failed: %v", err)
		}

		if result.Allowed {
			t.Error("Expensive endpoint should have lower limit")
		}
	})

	t.Run("very_expensive_endpoint", func(t *testing.T) {
		tb, _, cleanup := setupTestTokenBucket(t)
		defer cleanup()

		logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
		ml := ratelimit.NewMultiLayerLimiter(logger, tb)

		ctx := context.Background()
		tenantID := api.TenantID("tenant-exec")

		// Use the exact endpoint path that matches the switch case
		endpoint := "/v1/agents/{id}/exec"

		// This endpoint has 1/10 of base limit (Free: 10 req/s -> 1 req/s, burst 2)
		for i := 0; i < 2; i++ {
			result, err := ml.CheckLimits(ctx, tenantID, "", endpoint, ratelimit.TierFree)
			if err != nil {
				t.Fatalf("CheckLimits %d failed: %v", i, err)
			}
			if !result.Allowed {
				t.Errorf("Request %d should be allowed (burst=2)", i)
			}
		}

		// Third request should be denied at endpoint level
		result, err := ml.CheckLimits(ctx, tenantID, "", endpoint, ratelimit.TierFree)
		if err != nil {
			t.Fatalf("CheckLimits failed: %v", err)
		}

		if result.Allowed {
			t.Error("Very expensive endpoint should deny after exhausting burst capacity of 2")
		}
	})
}

// ============================================================================
// Middleware Tests
// ============================================================================

func TestRateLimitMiddleware(t *testing.T) {
	t.Run("request_allowed", func(t *testing.T) {
		tb, _, cleanup := setupTestTokenBucket(t)
		defer cleanup()

		logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
		ml := ratelimit.NewMultiLayerLimiter(logger, tb)

		middleware := ratelimit.Middleware(logger, ml)

		handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("OK"))
		}))

		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("X-Tenant-ID", "tenant-1")

		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Status code = %d, want %d", rec.Code, http.StatusOK)
		}

		// Check rate limit headers
		if rec.Header().Get("X-RateLimit-Limit") == "" {
			t.Error("X-RateLimit-Limit header not set")
		}
		if rec.Header().Get("X-RateLimit-Remaining") == "" {
			t.Error("X-RateLimit-Remaining header not set")
		}
		if rec.Header().Get("X-RateLimit-Reset") == "" {
			t.Error("X-RateLimit-Reset header not set")
		}
	})

	t.Run("request_denied", func(t *testing.T) {
		tb, _, cleanup := setupTestTokenBucket(t)
		defer cleanup()

		logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
		ml := ratelimit.NewMultiLayerLimiter(logger, tb)

		middleware := ratelimit.Middleware(logger, ml)

		handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("OK"))
		}))

		// Exhaust rate limit (Free tier: burst 20)
		for i := 0; i < 20; i++ {
			req := httptest.NewRequest("GET", "/test", nil)
			req.Header.Set("X-Tenant-ID", "tenant-exhausted")
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
		}

		// Next request should be denied
		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("X-Tenant-ID", "tenant-exhausted")

		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusTooManyRequests {
			t.Errorf("Status code = %d, want %d", rec.Code, http.StatusTooManyRequests)
		}

		if rec.Header().Get("Retry-After") == "" {
			t.Error("Retry-After header not set when rate limited")
		}
	})

	t.Run("no_tenant_id_skips_rate_limit", func(t *testing.T) {
		tb, _, cleanup := setupTestTokenBucket(t)
		defer cleanup()

		logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
		ml := ratelimit.NewMultiLayerLimiter(logger, tb)

		middleware := ratelimit.Middleware(logger, ml)

		handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		// Request without tenant ID
		req := httptest.NewRequest("GET", "/test", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Status code = %d, want %d (should skip rate limit)", rec.Code, http.StatusOK)
		}
	})
}

func TestWriteRateLimitHeaders(t *testing.T) {
	t.Run("writes_all_headers", func(t *testing.T) {
		rec := httptest.NewRecorder()

		result := &ratelimit.Result{
			Allowed:    true,
			Limit:      100,
			Remaining:  75,
			ResetAt:    time.Unix(1640000000, 0),
			RetryAfter: 0,
		}

		ratelimit.WriteRateLimitHeaders(rec, result)

		if rec.Header().Get("X-RateLimit-Limit") != "100" {
			t.Errorf("X-RateLimit-Limit = %s, want 100", rec.Header().Get("X-RateLimit-Limit"))
		}
		if rec.Header().Get("X-RateLimit-Remaining") != "75" {
			t.Errorf("X-RateLimit-Remaining = %s, want 75", rec.Header().Get("X-RateLimit-Remaining"))
		}
		if rec.Header().Get("X-RateLimit-Reset") != "1640000000" {
			t.Errorf("X-RateLimit-Reset = %s, want 1640000000", rec.Header().Get("X-RateLimit-Reset"))
		}
	})

	t.Run("includes_retry_after_when_denied", func(t *testing.T) {
		rec := httptest.NewRecorder()

		result := &ratelimit.Result{
			Allowed:    false,
			Limit:      100,
			Remaining:  0,
			ResetAt:    time.Unix(1640000000, 0),
			RetryAfter: 30 * time.Second,
		}

		ratelimit.WriteRateLimitHeaders(rec, result)

		if rec.Header().Get("Retry-After") != "30" {
			t.Errorf("Retry-After = %s, want 30", rec.Header().Get("Retry-After"))
		}
	})
}

func TestTooManyRequestsResponse(t *testing.T) {
	rec := httptest.NewRecorder()

	result := &ratelimit.Result{
		Allowed:    false,
		Limit:      100,
		Remaining:  0,
		ResetAt:    time.Unix(1640000000, 0),
		RetryAfter: 60 * time.Second,
	}

	ratelimit.TooManyRequestsResponse(rec, result)

	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("Status code = %d, want %d", rec.Code, http.StatusTooManyRequests)
	}

	if rec.Header().Get("Retry-After") != "60" {
		t.Errorf("Retry-After = %s, want 60", rec.Header().Get("Retry-After"))
	}

	body := rec.Body.String()
	if body == "" {
		t.Error("Response body is empty")
	}
}

// ============================================================================
// Edge Case Tests
// ============================================================================

func TestEdgeCases(t *testing.T) {
	t.Run("zero_burst_capacity", func(t *testing.T) {
		tb, _, cleanup := setupTestTokenBucket(t)
		defer cleanup()

		ctx := context.Background()
		limit := ratelimit.Limit{Rate: 10, Burst: 0, Period: time.Second}

		// With zero burst, no requests should be allowed
		result, err := tb.Allow(ctx, "user:zero-burst", limit)
		if err != nil {
			t.Fatalf("Allow failed: %v", err)
		}

		if result.Allowed {
			t.Error("Request should be denied with zero burst capacity")
		}
	})

	t.Run("very_high_rate", func(t *testing.T) {
		tb, _, cleanup := setupTestTokenBucket(t)
		defer cleanup()

		ctx := context.Background()
		limit := ratelimit.Limit{Rate: 10000, Burst: 20000, Period: time.Second}

		result, err := tb.Allow(ctx, "user:high-rate", limit)
		if err != nil {
			t.Fatalf("Allow failed: %v", err)
		}

		if !result.Allowed {
			t.Error("Request should be allowed with very high rate")
		}
	})
}

// ============================================================================
// AllowN Tests
// ============================================================================

func TestAllowN(t *testing.T) {
	t.Run("request_multiple_tokens", func(t *testing.T) {
		tb, _, cleanup := setupTestTokenBucket(t)
		defer cleanup()

		ctx := context.Background()
		limit := ratelimit.Limit{Rate: 10, Burst: 20, Period: time.Second}

		// AllowN currently just calls Allow (simplified implementation)
		result, err := tb.AllowN(ctx, "user:allowN", limit, 5)
		if err != nil {
			t.Fatalf("AllowN failed: %v", err)
		}

		// Should still work (even though it's simplified)
		if !result.Allowed {
			t.Error("AllowN should allow request")
		}
	})
}
