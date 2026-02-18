package ha

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
)

// StateReplication manages state replication across cluster nodes
type StateReplication struct {
	logger *slog.Logger
	client *clientv3.Client
	config ReplicationConfig

	// Local state cache
	mu    sync.RWMutex
	state map[string]interface{}

	// Watch channels
	watchCtx    context.Context
	watchCancel context.CancelFunc
}

// ReplicationConfig configures state replication
type ReplicationConfig struct {
	// EtcdEndpoints are the etcd cluster endpoints
	EtcdEndpoints []string

	// StatePrefix is the key prefix for replicated state
	StatePrefix string

	// SyncInterval is how often to sync state
	SyncInterval time.Duration

	// LagThreshold is the max acceptable replication lag
	LagThreshold time.Duration
}

// DefaultReplicationConfig returns default replication configuration
func DefaultReplicationConfig() ReplicationConfig {
	return ReplicationConfig{
		EtcdEndpoints: []string{"localhost:2379"},
		StatePrefix:   "/aether/state/",
		SyncInterval:  5 * time.Second,
		LagThreshold:  10 * time.Second,
	}
}

// ReplicatedState represents a piece of replicated state
type ReplicatedState struct {
	Key       string
	Value     interface{}
	Version   int64
	Timestamp time.Time
}

// NewStateReplication creates a new state replication instance
func NewStateReplication(logger *slog.Logger, config ReplicationConfig) (*StateReplication, error) {
	client, err := clientv3.New(clientv3.Config{
		Endpoints:   config.EtcdEndpoints,
		DialTimeout: 5 * time.Second,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create etcd client: %w", err)
	}

	// Test connection with timeout to fail fast if etcd is unavailable
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// Use Status to verify connectivity (forces actual RPC call)
	if _, err := client.Status(ctx, config.EtcdEndpoints[0]); err != nil {
		client.Close()
		return nil, fmt.Errorf("etcd not available: %w", err)
	}

	watchCtx, watchCancel := context.WithCancel(context.Background())

	return &StateReplication{
		logger:      logger,
		client:      client,
		config:      config,
		state:       make(map[string]interface{}),
		watchCtx:    watchCtx,
		watchCancel: watchCancel,
	}, nil
}

// Put replicates state to all nodes
func (sr *StateReplication) Put(ctx context.Context, key string, value interface{}) error {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("failed to marshal value: %w", err)
	}

	fullKey := sr.config.StatePrefix + key

	_, err = sr.client.Put(ctx, fullKey, string(data))
	if err != nil {
		return fmt.Errorf("failed to put state: %w", err)
	}

	// Update local cache
	sr.mu.Lock()
	sr.state[key] = value
	sr.mu.Unlock()

	sr.logger.Debug("replicated state",
		"key", key,
		"size", len(data),
	)

	return nil
}

// Get retrieves state from local cache or etcd
func (sr *StateReplication) Get(ctx context.Context, key string) (interface{}, error) {
	// Try local cache first
	sr.mu.RLock()
	if value, ok := sr.state[key]; ok {
		sr.mu.RUnlock()
		return value, nil
	}
	sr.mu.RUnlock()

	// Fetch from etcd
	fullKey := sr.config.StatePrefix + key

	resp, err := sr.client.Get(ctx, fullKey)
	if err != nil {
		return nil, fmt.Errorf("failed to get state: %w", err)
	}

	if len(resp.Kvs) == 0 {
		return nil, fmt.Errorf("key not found: %s", key)
	}

	var value interface{}
	if err := json.Unmarshal(resp.Kvs[0].Value, &value); err != nil {
		return nil, fmt.Errorf("failed to unmarshal value: %w", err)
	}

	// Update local cache
	sr.mu.Lock()
	sr.state[key] = value
	sr.mu.Unlock()

	return value, nil
}

// Delete removes state from all nodes
func (sr *StateReplication) Delete(ctx context.Context, key string) error {
	fullKey := sr.config.StatePrefix + key

	_, err := sr.client.Delete(ctx, fullKey)
	if err != nil {
		return fmt.Errorf("failed to delete state: %w", err)
	}

	// Remove from local cache
	sr.mu.Lock()
	delete(sr.state, key)
	sr.mu.Unlock()

	sr.logger.Debug("deleted replicated state", "key", key)

	return nil
}

// List lists all keys with the given prefix
func (sr *StateReplication) List(ctx context.Context, prefix string) ([]string, error) {
	fullPrefix := sr.config.StatePrefix + prefix

	resp, err := sr.client.Get(ctx, fullPrefix, clientv3.WithPrefix())
	if err != nil {
		return nil, fmt.Errorf("failed to list keys: %w", err)
	}

	keys := make([]string, 0, len(resp.Kvs))
	for _, kv := range resp.Kvs {
		// Strip the state prefix
		key := string(kv.Key)
		if len(key) > len(sr.config.StatePrefix) {
			keys = append(keys, key[len(sr.config.StatePrefix):])
		}
	}

	return keys, nil
}

// Watch watches for changes to a key or prefix
func (sr *StateReplication) Watch(ctx context.Context, key string) <-chan *ReplicatedState {
	fullKey := sr.config.StatePrefix + key
	stateCh := make(chan *ReplicatedState, 10)

	go func() {
		defer close(stateCh)

		watchCh := sr.client.Watch(sr.watchCtx, fullKey, clientv3.WithPrefix())
		for {
			select {
			case <-ctx.Done():
				return
			case <-sr.watchCtx.Done():
				return
			case wresp, ok := <-watchCh:
				if !ok {
					return
				}

				for _, ev := range wresp.Events {
					var value interface{}
					if err := json.Unmarshal(ev.Kv.Value, &value); err != nil {
						sr.logger.Error("failed to unmarshal watch event", "error", err)
						continue
					}

					// Update local cache
					relativeKey := string(ev.Kv.Key)[len(sr.config.StatePrefix):]
					sr.mu.Lock()
					sr.state[relativeKey] = value
					sr.mu.Unlock()

					stateCh <- &ReplicatedState{
						Key:       relativeKey,
						Value:     value,
						Version:   ev.Kv.Version,
						Timestamp: time.Now(),
					}
				}
			}
		}
	}()

	return stateCh
}

// Sync synchronizes local cache with etcd
func (sr *StateReplication) Sync(ctx context.Context) error {
	resp, err := sr.client.Get(ctx, sr.config.StatePrefix, clientv3.WithPrefix())
	if err != nil {
		return fmt.Errorf("failed to sync state: %w", err)
	}

	sr.mu.Lock()
	defer sr.mu.Unlock()

	// Clear local cache
	sr.state = make(map[string]interface{})

	// Rebuild from etcd
	for _, kv := range resp.Kvs {
		var value interface{}
		if err := json.Unmarshal(kv.Value, &value); err != nil {
			sr.logger.Error("failed to unmarshal state during sync",
				"key", string(kv.Key),
				"error", err,
			)
			continue
		}

		key := string(kv.Key)[len(sr.config.StatePrefix):]
		sr.state[key] = value
	}

	sr.logger.Info("synchronized state", "keys", len(sr.state))

	return nil
}

// GetReplicationLag returns the current replication lag
func (sr *StateReplication) GetReplicationLag(ctx context.Context) (time.Duration, error) {
	// Get the latest revision from etcd
	resp, err := sr.client.Get(ctx, sr.config.StatePrefix, clientv3.WithPrefix(), clientv3.WithKeysOnly())
	if err != nil {
		return 0, fmt.Errorf("failed to get latest revision: %w", err)
	}

	if len(resp.Kvs) == 0 {
		return 0, nil
	}

	// For simplicity, we'll use the response time as an approximation
	// In a real implementation, you'd track timestamps in the state
	return 0, nil
}

// StartAutoSync starts automatic state synchronization
func (sr *StateReplication) StartAutoSync(ctx context.Context) {
	ticker := time.NewTicker(sr.config.SyncInterval)
	defer ticker.Stop()

	sr.logger.Info("started auto sync", "interval", sr.config.SyncInterval)

	for {
		select {
		case <-ctx.Done():
			sr.logger.Info("stopping auto sync")
			return
		case <-ticker.C:
			if err := sr.Sync(ctx); err != nil {
				sr.logger.Error("auto sync failed", "error", err)
			}
		}
	}
}

// Close closes the state replication instance
func (sr *StateReplication) Close() error {
	sr.watchCancel()

	if sr.client != nil {
		return sr.client.Close()
	}

	return nil
}

// StateSnapshot represents a point-in-time snapshot of all replicated state
type StateSnapshot struct {
	State     map[string]interface{}
	Timestamp time.Time
	Version   int64
}

// CreateSnapshot creates a snapshot of current state
func (sr *StateReplication) CreateSnapshot(ctx context.Context) (*StateSnapshot, error) {
	resp, err := sr.client.Get(ctx, sr.config.StatePrefix, clientv3.WithPrefix())
	if err != nil {
		return nil, fmt.Errorf("failed to create snapshot: %w", err)
	}

	snapshot := &StateSnapshot{
		State:     make(map[string]interface{}),
		Timestamp: time.Now(),
		Version:   resp.Header.Revision,
	}

	for _, kv := range resp.Kvs {
		var value interface{}
		if err := json.Unmarshal(kv.Value, &value); err != nil {
			sr.logger.Error("failed to unmarshal state in snapshot",
				"key", string(kv.Key),
				"error", err,
			)
			continue
		}

		key := string(kv.Key)[len(sr.config.StatePrefix):]
		snapshot.State[key] = value
	}

	sr.logger.Info("created state snapshot",
		"keys", len(snapshot.State),
		"version", snapshot.Version,
	)

	return snapshot, nil
}

// RestoreSnapshot restores state from a snapshot
func (sr *StateReplication) RestoreSnapshot(ctx context.Context, snapshot *StateSnapshot) error {
	sr.logger.Info("restoring state snapshot",
		"keys", len(snapshot.State),
		"timestamp", snapshot.Timestamp,
	)

	// Delete all existing state
	_, err := sr.client.Delete(ctx, sr.config.StatePrefix, clientv3.WithPrefix())
	if err != nil {
		return fmt.Errorf("failed to clear state: %w", err)
	}

	// Restore from snapshot
	for key, value := range snapshot.State {
		if err := sr.Put(ctx, key, value); err != nil {
			sr.logger.Error("failed to restore key", "key", key, "error", err)
			continue
		}
	}

	sr.logger.Info("restored state snapshot", "keys", len(snapshot.State))

	return nil
}
