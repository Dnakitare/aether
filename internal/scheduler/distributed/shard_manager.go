package distributed

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
	"go.etcd.io/etcd/client/v3/concurrency"
)

// ShardInfo contains metadata about a scheduler shard.
type ShardInfo struct {
	SchedulerID  string    `json:"scheduler_id"`
	InstanceID   string    `json:"instance_id"`
	Hostname     string    `json:"hostname"`
	VNodes       []VNode   `json:"vnodes"`
	AssignedKeys []string  `json:"assigned_keys"` // Sample of assigned node IDs
	Heartbeat    time.Time `json:"heartbeat"`
	Status       string    `json:"status"` // active, draining, stopped
}

// VNode represents a virtual node in the consistent hash ring.
type VNode struct {
	Hash    uint32 `json:"hash"`
	VNodeID string `json:"vnode_id"`
}

// ShardManager manages scheduler shard registration and discovery via etcd.
type ShardManager struct {
	logger    *slog.Logger
	client    *clientv3.Client
	session   *concurrency.Session
	config    ShardConfig
	hashRing  *ConsistentHash
	leaseID   clientv3.LeaseID // Lease ID for this scheduler's etcd key
	mu        sync.RWMutex
	stopCh    chan struct{}
	wg        sync.WaitGroup
	onChanged func(members []string) // Callback when ring membership changes
}

// ShardConfig configures the shard manager.
type ShardConfig struct {
	// EtcdEndpoints are the etcd cluster endpoints
	EtcdEndpoints []string

	// KeyPrefix for shard data in etcd
	KeyPrefix string

	// SchedulerID is the unique identifier for this scheduler
	SchedulerID string

	// InstanceID is the unique identifier for this instance
	InstanceID string

	// Hostname of this scheduler instance
	Hostname string

	// SessionTTL is the session time-to-live in seconds
	SessionTTL int

	// HeartbeatInterval is how often to refresh the shard entry
	HeartbeatInterval time.Duration

	// VirtualNodes is the number of virtual nodes per scheduler
	VirtualNodes int
}

// DefaultShardConfig returns default shard configuration.
func DefaultShardConfig() ShardConfig {
	return ShardConfig{
		EtcdEndpoints:     []string{"localhost:2379"},
		KeyPrefix:         "/aether/scheduler/shards",
		SessionTTL:        30,
		HeartbeatInterval: 15 * time.Second,
		VirtualNodes:      100,
	}
}

// NewShardManager creates a new shard manager.
func NewShardManager(logger *slog.Logger, config ShardConfig) (*ShardManager, error) {
	client, err := clientv3.New(clientv3.Config{
		Endpoints:   config.EtcdEndpoints,
		DialTimeout: 5 * time.Second,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create etcd client: %w", err)
	}

	session, err := concurrency.NewSession(client, concurrency.WithTTL(config.SessionTTL))
	if err != nil {
		client.Close()
		return nil, fmt.Errorf("failed to create etcd session: %w", err)
	}

	hashRing := NewConsistentHash(config.VirtualNodes)

	return &ShardManager{
		logger:   logger.With("component", "shard_manager"),
		client:   client,
		session:  session,
		config:   config,
		hashRing: hashRing,
		stopCh:   make(chan struct{}),
	}, nil
}

// OnChanged sets the callback for when ring membership changes.
func (sm *ShardManager) OnChanged(callback func(members []string)) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.onChanged = callback
}

// Start starts the shard manager.
func (sm *ShardManager) Start(ctx context.Context) error {
	sm.logger.InfoContext(ctx, "starting shard manager",
		"scheduler_id", sm.config.SchedulerID,
		"instance_id", sm.config.InstanceID,
	)

	// Register this scheduler
	if err := sm.register(ctx); err != nil {
		return fmt.Errorf("failed to register shard: %w", err)
	}

	// Discover existing shards and build hash ring
	if err := sm.discoverShards(ctx); err != nil {
		return fmt.Errorf("failed to discover shards: %w", err)
	}

	// Start heartbeat goroutine
	sm.wg.Add(1)
	go sm.heartbeatLoop(ctx)

	// Start watch goroutine
	sm.wg.Add(1)
	go sm.watchShards(ctx)

	sm.logger.InfoContext(ctx, "shard manager started")
	return nil
}

// Stop stops the shard manager.
func (sm *ShardManager) Stop(ctx context.Context) error {
	sm.logger.InfoContext(ctx, "stopping shard manager")

	close(sm.stopCh)
	sm.wg.Wait()

	// Deregister this scheduler
	if err := sm.deregister(ctx); err != nil {
		sm.logger.WarnContext(ctx, "failed to deregister shard", "error", err)
	}

	if sm.session != nil {
		if err := sm.session.Close(); err != nil {
			sm.logger.WarnContext(ctx, "failed to close session", "error", err)
		}
	}

	if sm.client != nil {
		sm.client.Close()
	}

	sm.logger.InfoContext(ctx, "shard manager stopped")
	return nil
}

// Health checks connectivity to the etcd cluster.
func (sm *ShardManager) Health(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	_, err := sm.client.Get(ctx, "health-probe")
	if err != nil {
		return fmt.Errorf("etcd health check failed: %w", err)
	}
	return nil
}

// GetScheduler returns the scheduler responsible for the given key.
func (sm *ShardManager) GetScheduler(key string) string {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.hashRing.Get(key)
}

// IsOwner returns true if this scheduler owns the given key.
func (sm *ShardManager) IsOwner(key string) bool {
	scheduler := sm.GetScheduler(key)
	return scheduler == sm.config.SchedulerID
}

// Members returns all schedulers in the cluster.
func (sm *ShardManager) Members() []string {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.hashRing.Members()
}

// register registers this scheduler in etcd.
func (sm *ShardManager) register(ctx context.Context) error {
	key := sm.shardKey(sm.config.SchedulerID)

	// Build vnode information
	vnodes := make([]VNode, sm.config.VirtualNodes)
	for i := 0; i < sm.config.VirtualNodes; i++ {
		vnodeID := fmt.Sprintf("%s-v%03d", sm.config.SchedulerID, i)
		hash := sm.hashRing.hash(vnodeID)
		vnodes[i] = VNode{
			Hash:    hash,
			VNodeID: vnodeID,
		}
	}

	info := ShardInfo{
		SchedulerID:  sm.config.SchedulerID,
		InstanceID:   sm.config.InstanceID,
		Hostname:     sm.config.Hostname,
		VNodes:       vnodes,
		AssignedKeys: []string{}, // Populated later
		Heartbeat:    time.Now(),
		Status:       "active",
	}

	data, err := json.Marshal(info)
	if err != nil {
		return fmt.Errorf("failed to marshal shard info: %w", err)
	}

	// Write with lease
	lease := clientv3.NewLease(sm.client)
	grantResp, err := lease.Grant(ctx, int64(sm.config.SessionTTL))
	if err != nil {
		return fmt.Errorf("failed to grant lease: %w", err)
	}

	_, err = sm.client.Put(ctx, key, string(data), clientv3.WithLease(grantResp.ID))
	if err != nil {
		return fmt.Errorf("failed to put shard info: %w", err)
	}

	// Store lease ID for heartbeat refresh
	sm.mu.Lock()
	sm.leaseID = grantResp.ID
	sm.mu.Unlock()

	sm.logger.InfoContext(ctx, "registered shard",
		"scheduler_id", sm.config.SchedulerID,
		"key", key,
		"lease_id", grantResp.ID,
	)

	return nil
}

// deregister removes this scheduler from etcd.
func (sm *ShardManager) deregister(ctx context.Context) error {
	key := sm.shardKey(sm.config.SchedulerID)

	_, err := sm.client.Delete(ctx, key)
	if err != nil {
		return fmt.Errorf("failed to delete shard info: %w", err)
	}

	sm.logger.InfoContext(ctx, "deregistered shard",
		"scheduler_id", sm.config.SchedulerID,
		"key", key,
	)

	return nil
}

// discoverShards queries etcd for all existing shards and builds the hash ring.
func (sm *ShardManager) discoverShards(ctx context.Context) error {
	prefix := sm.config.KeyPrefix + "/"

	resp, err := sm.client.Get(ctx, prefix, clientv3.WithPrefix())
	if err != nil {
		return fmt.Errorf("failed to get shards: %w", err)
	}

	sm.mu.Lock()
	defer sm.mu.Unlock()

	// Clear existing ring
	sm.hashRing = NewConsistentHash(sm.config.VirtualNodes)

	// Add all active schedulers to ring
	for _, kv := range resp.Kvs {
		var info ShardInfo
		if err := json.Unmarshal(kv.Value, &info); err != nil {
			sm.logger.WarnContext(ctx, "failed to unmarshal shard info",
				"key", string(kv.Key),
				"error", err,
			)
			continue
		}

		if info.Status != "active" {
			continue
		}

		sm.hashRing.Add(info.SchedulerID)
		sm.logger.InfoContext(ctx, "discovered shard",
			"scheduler_id", info.SchedulerID,
			"instance_id", info.InstanceID,
		)
	}

	sm.logger.InfoContext(ctx, "shard discovery complete",
		"total_schedulers", sm.hashRing.Count(),
	)

	return nil
}

// heartbeatLoop periodically updates the heartbeat timestamp.
func (sm *ShardManager) heartbeatLoop(ctx context.Context) {
	defer sm.wg.Done()

	ticker := time.NewTicker(sm.config.HeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-sm.stopCh:
			return
		case <-ticker.C:
			if err := sm.updateHeartbeat(ctx); err != nil {
				sm.logger.WarnContext(ctx, "failed to update heartbeat", "error", err)
			}
		}
	}
}

// updateHeartbeat updates the heartbeat timestamp in etcd.
func (sm *ShardManager) updateHeartbeat(ctx context.Context) error {
	key := sm.shardKey(sm.config.SchedulerID)

	// Get current value
	resp, err := sm.client.Get(ctx, key)
	if err != nil {
		return fmt.Errorf("failed to get shard info: %w", err)
	}

	if len(resp.Kvs) == 0 {
		// Shard entry disappeared - re-register
		sm.logger.WarnContext(ctx, "shard entry missing, re-registering")
		return sm.register(ctx)
	}

	var info ShardInfo
	if err := json.Unmarshal(resp.Kvs[0].Value, &info); err != nil {
		return fmt.Errorf("failed to unmarshal shard info: %w", err)
	}

	// Update heartbeat
	info.Heartbeat = time.Now()

	data, err := json.Marshal(info)
	if err != nil {
		return fmt.Errorf("failed to marshal shard info: %w", err)
	}

	// Get existing lease and refresh its TTL
	leaseID := clientv3.LeaseID(resp.Kvs[0].Lease)

	// Refresh lease TTL using KeepAliveOnce
	leaseClient := clientv3.NewLease(sm.client)
	_, err = leaseClient.KeepAliveOnce(ctx, leaseID)
	if err != nil {
		sm.logger.WarnContext(ctx, "failed to keep lease alive, re-registering", "error", err)
		return sm.register(ctx)
	}

	// Update the heartbeat timestamp in the value
	_, err = sm.client.Put(ctx, key, string(data), clientv3.WithLease(leaseID))
	if err != nil {
		return fmt.Errorf("failed to update heartbeat: %w", err)
	}

	sm.logger.DebugContext(ctx, "heartbeat updated",
		"scheduler_id", sm.config.SchedulerID,
	)

	return nil
}

// watchShards watches for changes to shard registrations in etcd.
func (sm *ShardManager) watchShards(ctx context.Context) {
	defer sm.wg.Done()

	prefix := sm.config.KeyPrefix + "/"
	watchCh := sm.client.Watch(ctx, prefix, clientv3.WithPrefix())

	sm.logger.InfoContext(ctx, "watching shards", "prefix", prefix)

	for {
		select {
		case <-ctx.Done():
			return
		case <-sm.stopCh:
			return
		case watchResp := <-watchCh:
			if watchResp.Err() != nil {
				sm.logger.ErrorContext(ctx, "watch error", "error", watchResp.Err())
				continue
			}

			for _, event := range watchResp.Events {
				sm.handleShardEvent(ctx, event)
			}
		}
	}
}

// handleShardEvent handles a shard change event.
func (sm *ShardManager) handleShardEvent(ctx context.Context, event *clientv3.Event) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	switch event.Type {
	case clientv3.EventTypePut:
		var info ShardInfo
		if err := json.Unmarshal(event.Kv.Value, &info); err != nil {
			sm.logger.WarnContext(ctx, "failed to unmarshal shard info",
				"key", string(event.Kv.Key),
				"error", err,
			)
			return
		}

		if info.Status == "active" {
			sm.hashRing.Add(info.SchedulerID)
			sm.logger.InfoContext(ctx, "scheduler joined",
				"scheduler_id", info.SchedulerID,
				"instance_id", info.InstanceID,
			)
		} else {
			sm.hashRing.Remove(info.SchedulerID)
			sm.logger.InfoContext(ctx, "scheduler status changed",
				"scheduler_id", info.SchedulerID,
				"status", info.Status,
			)
		}

	case clientv3.EventTypeDelete:
		// Extract scheduler ID from key
		key := string(event.Kv.Key)
		schedulerID := key[len(sm.config.KeyPrefix)+1:]

		sm.hashRing.Remove(schedulerID)
		sm.logger.InfoContext(ctx, "scheduler left",
			"scheduler_id", schedulerID,
		)
	}

	// Notify callback
	if sm.onChanged != nil {
		members := sm.hashRing.Members()
		go sm.onChanged(members)
	}
}

// shardKey returns the etcd key for a scheduler's shard info.
func (sm *ShardManager) shardKey(schedulerID string) string {
	return fmt.Sprintf("%s/%s", sm.config.KeyPrefix, schedulerID)
}

// GetShardInfo returns the shard info for a scheduler.
func (sm *ShardManager) GetShardInfo(ctx context.Context, schedulerID string) (*ShardInfo, error) {
	key := sm.shardKey(schedulerID)

	resp, err := sm.client.Get(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("failed to get shard info: %w", err)
	}

	if len(resp.Kvs) == 0 {
		return nil, fmt.Errorf("shard not found: %s", schedulerID)
	}

	var info ShardInfo
	if err := json.Unmarshal(resp.Kvs[0].Value, &info); err != nil {
		return nil, fmt.Errorf("failed to unmarshal shard info: %w", err)
	}

	return &info, nil
}

// ListShards returns all active shards.
func (sm *ShardManager) ListShards(ctx context.Context) ([]*ShardInfo, error) {
	prefix := sm.config.KeyPrefix + "/"

	resp, err := sm.client.Get(ctx, prefix, clientv3.WithPrefix())
	if err != nil {
		return nil, fmt.Errorf("failed to list shards: %w", err)
	}

	shards := make([]*ShardInfo, 0, len(resp.Kvs))
	for _, kv := range resp.Kvs {
		var info ShardInfo
		if err := json.Unmarshal(kv.Value, &info); err != nil {
			sm.logger.WarnContext(ctx, "failed to unmarshal shard info",
				"key", string(kv.Key),
				"error", err,
			)
			continue
		}

		shards = append(shards, &info)
	}

	return shards, nil
}
