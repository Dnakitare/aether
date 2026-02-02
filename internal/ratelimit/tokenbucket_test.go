package ratelimit_test

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/aether-runtime/aether/internal/ratelimit"
	"github.com/aether-runtime/aether/pkg/api"
)

func TestTokenBucket(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping Redis integration test in short mode")
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	// Connect to Redis
	redisClient := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})

	ctx := context.Background()
	if err := redisClient.Ping(ctx).Err(); err != nil {
		t.Skipf("Redis not available: %v", err)
	}
	defer redisClient.Close()

	config := ratelimit.Config{
		DefaultRate:  10,
		DefaultBurst: 20,
		KeyPrefix:    "test:",
	}

	tb := ratelimit.NewTokenBucket(logger, redisClient, config)

	// Test basic rate limiting
	limit := ratelimit.Limit{
		Rate:   5,
		Burst:  10,
		Period: time.Second,
	}

	// First request should be allowed
	result, err := tb.Allow(ctx, "test-key", limit)
	if err != nil {
		t.Fatalf("Failed to check rate limit: %v", err)
	}

	if !result.Allowed {
		t.Error("Expected first request to be allowed")
	}

	if result.Limit != 10 {
		t.Errorf("Expected limit = 10, got %d", result.Limit)
	}

	// Clean up
	tb.Reset(ctx, "test-key")
}

func TestTokenBucketTiers(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	redisClient := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})
	defer redisClient.Close()

	config := ratelimit.Config{}
	tb := ratelimit.NewTokenBucket(logger, redisClient, config)

	// Test tier limits
	tiers := []ratelimit.Tier{
		ratelimit.TierFree,
		ratelimit.TierPro,
		ratelimit.TierEnterprise,
	}

	for _, tier := range tiers {
		limit := tb.GetLimitForTier(tier)
		if limit.Rate == 0 {
			t.Errorf("Expected non-zero rate for tier %s", tier)
		}
		if limit.Burst == 0 {
			t.Errorf("Expected non-zero burst for tier %s", tier)
		}
	}
}

func TestMultiLayerLimiter(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping Redis integration test in short mode")
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	redisClient := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})

	ctx := context.Background()
	if err := redisClient.Ping(ctx).Err(); err != nil {
		t.Skipf("Redis not available: %v", err)
	}
	defer redisClient.Close()

	config := ratelimit.Config{
		KeyPrefix: "test:",
	}

	tb := ratelimit.NewTokenBucket(logger, redisClient, config)
	ml := ratelimit.NewMultiLayerLimiter(logger, tb)

	tenantID := api.TenantID("test-tenant")
	userID := "test-user"
	endpoint := "/v1/agents"

	// Check limits
	result, err := ml.CheckLimits(ctx, tenantID, userID, endpoint, ratelimit.TierFree)
	if err != nil {
		t.Fatalf("Failed to check limits: %v", err)
	}

	if !result.Allowed {
		t.Error("Expected request to be allowed")
	}

	// Clean up
	tb.Reset(ctx, "tenant:"+string(tenantID))
	tb.Reset(ctx, "user:"+string(tenantID)+":"+userID)
	tb.Reset(ctx, "endpoint:"+string(tenantID)+":"+endpoint)
}
