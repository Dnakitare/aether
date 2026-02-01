// Package state provides state persistence with Redis.
package state

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/dnakitare/aether/pkg/api"
)

// RedisStore provides state persistence using Redis.
type RedisStore struct {
	logger *slog.Logger
	client *redis.Client
	config Config
}

// Config holds Redis configuration.
type Config struct {
	// Address is the Redis server address (e.g., "localhost:6379")
	Address string

	// Password for authentication
	Password string

	// DB is the Redis database number
	DB int

	// KeyPrefix for namespacing keys
	KeyPrefix string

	// DefaultTTL for cached entries
	DefaultTTL time.Duration
}

// NewRedisStore creates a new Redis store.
func NewRedisStore(logger *slog.Logger, config Config) (*RedisStore, error) {
	if config.Address == "" {
		return nil, fmt.Errorf("redis address is required")
	}

	if config.KeyPrefix == "" {
		config.KeyPrefix = "aether:"
	}

	if config.DefaultTTL == 0 {
		config.DefaultTTL = 24 * time.Hour
	}

	client := redis.NewClient(&redis.Options{
		Addr:     config.Address,
		Password: config.Password,
		DB:       config.DB,
	})

	// Test connection
	ctx := context.Background()
	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to redis: %w", err)
	}

	return &RedisStore{
		logger: logger.With("component", "redis_store"),
		client: client,
		config: config,
	}, nil
}

// SaveAgentState saves agent state to Redis.
func (rs *RedisStore) SaveAgentState(ctx context.Context, info *api.AgentInfo) error {
	key := rs.agentKey(info.Config.ID)

	data, err := json.Marshal(info)
	if err != nil {
		return fmt.Errorf("failed to marshal agent info: %w", err)
	}

	if err := rs.client.Set(ctx, key, data, rs.config.DefaultTTL).Err(); err != nil {
		return fmt.Errorf("failed to save agent state: %w", err)
	}

	// Add to tenant's agent set
	tenantKey := rs.tenantAgentsKey(info.Config.TenantID)
	if err := rs.client.SAdd(ctx, tenantKey, info.Config.ID).Err(); err != nil {
		rs.logger.WarnContext(ctx, "failed to add agent to tenant set", "error", err)
	}

	rs.logger.DebugContext(ctx, "agent state saved", "agent_id", info.Config.ID)
	return nil
}

// GetAgentState retrieves agent state from Redis.
func (rs *RedisStore) GetAgentState(ctx context.Context, agentID api.AgentID) (*api.AgentInfo, error) {
	key := rs.agentKey(agentID)

	data, err := rs.client.Get(ctx, key).Bytes()
	if err == redis.Nil {
		return nil, fmt.Errorf("agent state not found: %s", agentID)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get agent state: %w", err)
	}

	var info api.AgentInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return nil, fmt.Errorf("failed to unmarshal agent info: %w", err)
	}

	return &info, nil
}

// DeleteAgentState deletes agent state from Redis.
func (rs *RedisStore) DeleteAgentState(ctx context.Context, agentID api.AgentID, tenantID api.TenantID) error {
	key := rs.agentKey(agentID)

	if err := rs.client.Del(ctx, key).Err(); err != nil {
		return fmt.Errorf("failed to delete agent state: %w", err)
	}

	// Remove from tenant's agent set
	tenantKey := rs.tenantAgentsKey(tenantID)
	if err := rs.client.SRem(ctx, tenantKey, agentID).Err(); err != nil {
		rs.logger.WarnContext(ctx, "failed to remove agent from tenant set", "error", err)
	}

	rs.logger.DebugContext(ctx, "agent state deleted", "agent_id", agentID)
	return nil
}

// ListAgentsByTenant lists all agents for a tenant.
func (rs *RedisStore) ListAgentsByTenant(ctx context.Context, tenantID api.TenantID) ([]api.AgentID, error) {
	key := rs.tenantAgentsKey(tenantID)

	members, err := rs.client.SMembers(ctx, key).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to list agents: %w", err)
	}

	agentIDs := make([]api.AgentID, len(members))
	for i, member := range members {
		agentIDs[i] = api.AgentID(member)
	}

	return agentIDs, nil
}

// Lock acquires a distributed lock.
func (rs *RedisStore) Lock(ctx context.Context, name string, ttl time.Duration) (bool, error) {
	key := rs.lockKey(name)

	// Try to acquire lock using SET NX (set if not exists)
	ok, err := rs.client.SetNX(ctx, key, "locked", ttl).Result()
	if err != nil {
		return false, fmt.Errorf("failed to acquire lock: %w", err)
	}

	if ok {
		rs.logger.DebugContext(ctx, "lock acquired", "name", name)
	}

	return ok, nil
}

// Unlock releases a distributed lock.
func (rs *RedisStore) Unlock(ctx context.Context, name string) error {
	key := rs.lockKey(name)

	if err := rs.client.Del(ctx, key).Err(); err != nil {
		return fmt.Errorf("failed to release lock: %w", err)
	}

	rs.logger.DebugContext(ctx, "lock released", "name", name)
	return nil
}

// SetSession stores a session.
func (rs *RedisStore) SetSession(ctx context.Context, sessionID string, data map[string]interface{}, ttl time.Duration) error {
	key := rs.sessionKey(sessionID)

	jsonData, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal session data: %w", err)
	}

	if err := rs.client.Set(ctx, key, jsonData, ttl).Err(); err != nil {
		return fmt.Errorf("failed to save session: %w", err)
	}

	return nil
}

// GetSession retrieves a session.
func (rs *RedisStore) GetSession(ctx context.Context, sessionID string) (map[string]interface{}, error) {
	key := rs.sessionKey(sessionID)

	data, err := rs.client.Get(ctx, key).Bytes()
	if err == redis.Nil {
		return nil, fmt.Errorf("session not found: %s", sessionID)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get session: %w", err)
	}

	var sessionData map[string]interface{}
	if err := json.Unmarshal(data, &sessionData); err != nil {
		return nil, fmt.Errorf("failed to unmarshal session data: %w", err)
	}

	return sessionData, nil
}

// DeleteSession deletes a session.
func (rs *RedisStore) DeleteSession(ctx context.Context, sessionID string) error {
	key := rs.sessionKey(sessionID)

	if err := rs.client.Del(ctx, key).Err(); err != nil {
		return fmt.Errorf("failed to delete session: %w", err)
	}

	return nil
}

// IncrementCounter atomically increments a counter.
func (rs *RedisStore) IncrementCounter(ctx context.Context, name string) (int64, error) {
	key := rs.counterKey(name)

	count, err := rs.client.Incr(ctx, key).Result()
	if err != nil {
		return 0, fmt.Errorf("failed to increment counter: %w", err)
	}

	return count, nil
}

// GetCounter gets a counter value.
func (rs *RedisStore) GetCounter(ctx context.Context, name string) (int64, error) {
	key := rs.counterKey(name)

	count, err := rs.client.Get(ctx, key).Int64()
	if err == redis.Nil {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("failed to get counter: %w", err)
	}

	return count, nil
}

// SetWithExpiry sets a value with expiration.
func (rs *RedisStore) SetWithExpiry(ctx context.Context, key string, value interface{}, ttl time.Duration) error {
	fullKey := rs.config.KeyPrefix + key

	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("failed to marshal value: %w", err)
	}

	if err := rs.client.Set(ctx, fullKey, data, ttl).Err(); err != nil {
		return fmt.Errorf("failed to set value: %w", err)
	}

	return nil
}

// Get retrieves a value.
func (rs *RedisStore) Get(ctx context.Context, key string, dest interface{}) error {
	fullKey := rs.config.KeyPrefix + key

	data, err := rs.client.Get(ctx, fullKey).Bytes()
	if err == redis.Nil {
		return fmt.Errorf("key not found: %s", key)
	}
	if err != nil {
		return fmt.Errorf("failed to get value: %w", err)
	}

	if err := json.Unmarshal(data, dest); err != nil {
		return fmt.Errorf("failed to unmarshal value: %w", err)
	}

	return nil
}

// Delete deletes a value.
func (rs *RedisStore) Delete(ctx context.Context, key string) error {
	fullKey := rs.config.KeyPrefix + key

	if err := rs.client.Del(ctx, fullKey).Err(); err != nil {
		return fmt.Errorf("failed to delete value: %w", err)
	}

	return nil
}

// Close closes the Redis connection.
func (rs *RedisStore) Close() error {
	return rs.client.Close()
}

// Health checks if Redis is healthy.
func (rs *RedisStore) Health(ctx context.Context) error {
	if err := rs.client.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("redis health check failed: %w", err)
	}
	return nil
}

// Key builders

func (rs *RedisStore) agentKey(agentID api.AgentID) string {
	return fmt.Sprintf("%sagent:%s", rs.config.KeyPrefix, agentID)
}

func (rs *RedisStore) tenantAgentsKey(tenantID api.TenantID) string {
	return fmt.Sprintf("%stenant:%s:agents", rs.config.KeyPrefix, tenantID)
}

func (rs *RedisStore) lockKey(name string) string {
	return fmt.Sprintf("%slock:%s", rs.config.KeyPrefix, name)
}

func (rs *RedisStore) sessionKey(sessionID string) string {
	return fmt.Sprintf("%ssession:%s", rs.config.KeyPrefix, sessionID)
}

func (rs *RedisStore) counterKey(name string) string {
	return fmt.Sprintf("%scounter:%s", rs.config.KeyPrefix, name)
}
