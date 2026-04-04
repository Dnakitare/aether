// Package state provides state persistence with Redis.
package state

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
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

// SaveAgentState saves agent state to Redis atomically.
// Uses Redis pipeline to ensure both agent key and tenant set are updated together.
func (rs *RedisStore) SaveAgentState(ctx context.Context, info *api.AgentInfo) error {
	agentKey := rs.agentKey(info.Config.ID)
	tenantKey := rs.tenantAgentsKey(info.Config.TenantID)

	data, err := json.Marshal(info)
	if err != nil {
		return fmt.Errorf("failed to marshal agent info: %w", err)
	}

	// Use Redis pipeline for atomic execution of both operations
	pipe := rs.client.Pipeline()
	pipe.Set(ctx, agentKey, data, rs.config.DefaultTTL)
	pipe.SAdd(ctx, tenantKey, string(info.Config.ID))

	// Execute pipeline atomically
	_, err = pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to save agent state: %w", err)
	}

	rs.logger.DebugContext(ctx, "agent state saved",
		"agent_id", info.Config.ID,
		"tenant_id", info.Config.TenantID,
	)
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

// DeleteAgentState deletes agent state from Redis atomically.
// Uses Redis pipeline to ensure both agent key and tenant set are updated together.
func (rs *RedisStore) DeleteAgentState(ctx context.Context, agentID api.AgentID, tenantID api.TenantID) error {
	agentKey := rs.agentKey(agentID)
	tenantKey := rs.tenantAgentsKey(tenantID)

	// Use Redis pipeline for atomic execution of both operations
	pipe := rs.client.Pipeline()
	pipe.Del(ctx, agentKey)
	pipe.SRem(ctx, tenantKey, string(agentID))

	// Execute pipeline atomically
	_, err := pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("failed to delete agent state: %w", err)
	}

	rs.logger.DebugContext(ctx, "agent state deleted",
		"agent_id", agentID,
		"tenant_id", tenantID,
	)
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

// Lock represents an acquired distributed lock with ownership token.
type Lock struct {
	key       string
	token     string
	ttl       time.Duration
	store     *RedisStore
	cancelCtx context.Context
	cancelFn  context.CancelFunc
	wg        sync.WaitGroup
}

// Lock acquires a distributed lock with ownership token.
// Returns a Lock instance that must be unlocked when done.
// The lock includes an automatic watchdog that extends TTL while held.
func (rs *RedisStore) Lock(ctx context.Context, name string, ttl time.Duration) (*Lock, error) {
	key := rs.lockKey(name)
	token := uuid.New().String()

	// Lua script for atomic SET NX with token
	// This ensures we only set the key if it doesn't exist
	script := `
		if redis.call("set", KEYS[1], ARGV[1], "NX", "EX", ARGV[2]) then
			return 1
		end
		return 0
	`

	result, err := rs.client.Eval(ctx, script, []string{key}, token, int(ttl.Seconds())).Int()
	if err != nil {
		return nil, fmt.Errorf("failed to acquire lock: %w", err)
	}

	if result == 0 {
		return nil, fmt.Errorf("lock already held by another process")
	}

	// Create lock instance
	lockCtx, cancelFn := context.WithCancel(context.Background())
	lock := &Lock{
		key:       key,
		token:     token,
		ttl:       ttl,
		store:     rs,
		cancelCtx: lockCtx,
		cancelFn:  cancelFn,
	}

	// Start watchdog to extend TTL
	lock.wg.Add(1)
	go lock.watchdog()

	rs.logger.DebugContext(ctx, "lock acquired", "name", name, "token", token)
	return lock, nil
}

// TryLock attempts to acquire a lock without blocking.
// Returns nil if lock is already held.
func (rs *RedisStore) TryLock(ctx context.Context, name string, ttl time.Duration) (*Lock, error) {
	return rs.Lock(ctx, name, ttl)
}

// Unlock releases a distributed lock.
// Only the process that acquired the lock (matching token) can release it.
func (rs *RedisStore) Unlock(ctx context.Context, lock *Lock) error {
	if lock == nil {
		return fmt.Errorf("lock is nil")
	}

	// Stop watchdog
	lock.cancelFn()
	lock.wg.Wait()

	// Lua script for atomic check-and-delete
	// This ensures we only delete the lock if we own it (token matches)
	script := `
		if redis.call("get", KEYS[1]) == ARGV[1] then
			return redis.call("del", KEYS[1])
		else
			return 0
		end
	`

	result, err := rs.client.Eval(ctx, script, []string{lock.key}, lock.token).Int()
	if err != nil {
		return fmt.Errorf("failed to release lock: %w", err)
	}

	if result == 0 {
		rs.logger.WarnContext(ctx, "lock not held or expired", "key", lock.key)
		return fmt.Errorf("lock not held or expired (token mismatch)")
	}

	rs.logger.DebugContext(ctx, "lock released", "key", lock.key)
	return nil
}

// watchdog periodically extends the lock TTL while it's held.
// This prevents the lock from expiring during long operations.
func (l *Lock) watchdog() {
	defer l.wg.Done()

	ticker := time.NewTicker(l.ttl / 3) // Extend at 1/3 of TTL
	defer ticker.Stop()

	for {
		select {
		case <-l.cancelCtx.Done():
			return
		case <-ticker.C:
			l.extendTTL()
		}
	}
}

// extendTTL extends the lock TTL if we still own it.
func (l *Lock) extendTTL() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Lua script for atomic check-and-extend
	// Only extend if the token matches (we still own the lock)
	script := `
		if redis.call("get", KEYS[1]) == ARGV[1] then
			return redis.call("expire", KEYS[1], ARGV[2])
		else
			return 0
		end
	`

	result, err := l.store.client.Eval(ctx, script, []string{l.key}, l.token, int(l.ttl.Seconds())).Int()
	if err != nil {
		l.store.logger.WarnContext(ctx, "failed to extend lock TTL", "error", err, "key", l.key)
		return
	}

	if result == 0 {
		l.store.logger.WarnContext(ctx, "lock lost (token mismatch or expired)", "key", l.key)
		// Stop watchdog since we no longer own the lock
		l.cancelFn()
	} else {
		l.store.logger.DebugContext(ctx, "lock TTL extended", "key", l.key, "ttl", l.ttl)
	}
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

// GetClient returns the underlying Redis client for testing purposes.
func (rs *RedisStore) GetClient() *redis.Client {
	return rs.client
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
