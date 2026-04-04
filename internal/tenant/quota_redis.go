package tenant

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/go-redis/redis/v8"

	"github.com/dnakitare/aether/pkg/api"
)

// RedisQuotaStore persists usage counters in Redis so they survive restarts
// and are consistent across multiple server instances.
//
// Quota definitions (limits) remain in the in-memory QuotaManager; only the
// live usage counters are stored in Redis. This keeps the hot read path fast
// while making allocations durable and distributed.
type RedisQuotaStore struct {
	client    *redis.Client
	keyPrefix string
	ttl       time.Duration
}

// NewRedisQuotaStore creates a quota store backed by Redis.
// keyPrefix namespaces all keys (e.g. "aether:quota").
// ttl is applied to usage keys so they self-expire if not refreshed
// (set to 0 to disable expiry).
func NewRedisQuotaStore(client *redis.Client, keyPrefix string, ttl time.Duration) *RedisQuotaStore {
	return &RedisQuotaStore{
		client:    client,
		keyPrefix: keyPrefix,
		ttl:       ttl,
	}
}

// usageKey returns the Redis key for a tenant's usage field.
func (rs *RedisQuotaStore) usageKey(tenantID api.TenantID) string {
	return fmt.Sprintf("%s:usage:%s", rs.keyPrefix, tenantID)
}

// GetUsage returns current resource usage for a tenant from Redis.
func (rs *RedisQuotaStore) GetUsage(ctx context.Context, tenantID api.TenantID) (*ResourceUsage, error) {
	key := rs.usageKey(tenantID)
	vals, err := rs.client.HGetAll(ctx, key).Result()
	if err != nil {
		return nil, fmt.Errorf("redis HGetAll failed: %w", err)
	}

	usage := &ResourceUsage{
		TenantID:    tenantID,
		LastUpdated: time.Now(),
	}

	if v, ok := vals["agents"]; ok {
		usage.AgentCount, _ = strconv.Atoi(v)
	}
	if v, ok := vals["cpu"]; ok {
		usage.CPUCores, _ = strconv.ParseInt(v, 10, 64)
	}
	if v, ok := vals["memory"]; ok {
		usage.MemoryMB, _ = strconv.ParseInt(v, 10, 64)
	}
	if v, ok := vals["disk"]; ok {
		usage.DiskMB, _ = strconv.ParseInt(v, 10, 64)
	}

	return usage, nil
}

// AllocateResources atomically increments usage counters in Redis.
func (rs *RedisQuotaStore) AllocateResources(ctx context.Context, tenantID api.TenantID, req ResourceRequest) error {
	key := rs.usageKey(tenantID)

	pipe := rs.client.Pipeline()
	pipe.HIncrBy(ctx, key, "agents", int64(req.AgentCount))
	pipe.HIncrBy(ctx, key, "cpu", req.CPUCores)
	pipe.HIncrBy(ctx, key, "memory", req.MemoryMB)
	pipe.HIncrBy(ctx, key, "disk", req.DiskMB)
	if rs.ttl > 0 {
		pipe.Expire(ctx, key, rs.ttl)
	}
	_, err := pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("redis pipeline failed during allocation: %w", err)
	}
	return nil
}

// ReleaseResources atomically decrements usage counters in Redis, flooring at zero.
func (rs *RedisQuotaStore) ReleaseResources(ctx context.Context, tenantID api.TenantID, req ResourceRequest) error {
	key := rs.usageKey(tenantID)

	// Lua script: decrement but clamp to 0 to avoid negative counters.
	script := redis.NewScript(`
local key = KEYS[1]
local fields = {"agents","cpu","memory","disk"}
local deltas = {tonumber(ARGV[1]), tonumber(ARGV[2]), tonumber(ARGV[3]), tonumber(ARGV[4])}
for i, field in ipairs(fields) do
  local cur = tonumber(redis.call("HGET", key, field) or "0")
  local next = math.max(0, cur - deltas[i])
  redis.call("HSET", key, field, tostring(next))
end
return 1
`)

	err := script.Run(ctx, rs.client, []string{key},
		req.AgentCount, req.CPUCores, req.MemoryMB, req.DiskMB,
	).Err()
	if err != nil && err != redis.Nil {
		return fmt.Errorf("redis release script failed: %w", err)
	}
	return nil
}

// InitUsage ensures a usage entry exists for the tenant (idempotent).
func (rs *RedisQuotaStore) InitUsage(ctx context.Context, tenantID api.TenantID) error {
	key := rs.usageKey(tenantID)
	// HSETNX only sets fields that don't already exist.
	pipe := rs.client.Pipeline()
	pipe.HSetNX(ctx, key, "agents", 0)
	pipe.HSetNX(ctx, key, "cpu", 0)
	pipe.HSetNX(ctx, key, "memory", 0)
	pipe.HSetNX(ctx, key, "disk", 0)
	if rs.ttl > 0 {
		pipe.Expire(ctx, key, rs.ttl)
	}
	_, err := pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("redis init usage failed: %w", err)
	}
	return nil
}
