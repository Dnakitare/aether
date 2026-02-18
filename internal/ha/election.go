package ha

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
	"go.etcd.io/etcd/client/v3/concurrency"
)

// LeaderElection manages leader election using etcd
type LeaderElection struct {
	logger   *slog.Logger
	client   *clientv3.Client
	session  *concurrency.Session
	election *concurrency.Election
	config   ElectionConfig

	// Callbacks
	onBecomeLeader   func(context.Context) error
	onLoseLeadership func(context.Context) error
}

// ElectionConfig configures leader election
type ElectionConfig struct {
	// EtcdEndpoints are the etcd cluster endpoints
	EtcdEndpoints []string

	// ElectionKey is the key used for leader election
	ElectionKey string

	// SessionTTL is the session time-to-live in seconds
	SessionTTL int

	// LeaderName is the name of this candidate
	LeaderName string
}

// DefaultElectionConfig returns default election configuration
func DefaultElectionConfig() ElectionConfig {
	return ElectionConfig{
		EtcdEndpoints: []string{"localhost:2379"},
		ElectionKey:   "/aether/leader",
		SessionTTL:    10,
		LeaderName:    "aether-node",
	}
}

// NewLeaderElection creates a new leader election instance
func NewLeaderElection(logger *slog.Logger, config ElectionConfig) (*LeaderElection, error) {
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

	session, err := concurrency.NewSession(client, concurrency.WithTTL(config.SessionTTL))
	if err != nil {
		client.Close()
		return nil, fmt.Errorf("failed to create etcd session: %w", err)
	}

	election := concurrency.NewElection(session, config.ElectionKey)

	return &LeaderElection{
		logger:   logger,
		client:   client,
		session:  session,
		election: election,
		config:   config,
	}, nil
}

// OnBecomeLeader sets the callback for when this node becomes leader
func (le *LeaderElection) OnBecomeLeader(callback func(context.Context) error) {
	le.onBecomeLeader = callback
}

// OnLoseLeadership sets the callback for when this node loses leadership
func (le *LeaderElection) OnLoseLeadership(callback func(context.Context) error) {
	le.onLoseLeadership = callback
}

// Campaign starts the leader election campaign
func (le *LeaderElection) Campaign(ctx context.Context) error {
	le.logger.Info("starting leader election campaign",
		"key", le.config.ElectionKey,
		"name", le.config.LeaderName,
	)

	// Campaign for leadership
	if err := le.election.Campaign(ctx, le.config.LeaderName); err != nil {
		return fmt.Errorf("campaign failed: %w", err)
	}

	le.logger.Info("became leader",
		"key", le.config.ElectionKey,
		"name", le.config.LeaderName,
	)

	// Call become leader callback
	if le.onBecomeLeader != nil {
		if err := le.onBecomeLeader(ctx); err != nil {
			le.logger.Error("become leader callback failed", "error", err)
			return err
		}
	}

	return nil
}

// Observe watches for leader changes
func (le *LeaderElection) Observe(ctx context.Context) (<-chan string, error) {
	observeCh := make(chan string, 1)

	go func() {
		defer close(observeCh)

		ch := le.election.Observe(ctx)
		for resp := range ch {
			if len(resp.Kvs) > 0 {
				leader := string(resp.Kvs[0].Value)
				le.logger.Info("leader changed", "leader", leader)
				observeCh <- leader
			}
		}
	}()

	return observeCh, nil
}

// IsLeader returns true if this node is the current leader
func (le *LeaderElection) IsLeader(ctx context.Context) (bool, error) {
	resp, err := le.election.Leader(ctx)
	if err != nil {
		if err == concurrency.ErrElectionNoLeader {
			return false, nil
		}
		return false, err
	}

	if len(resp.Kvs) == 0 {
		return false, nil
	}

	return string(resp.Kvs[0].Value) == le.config.LeaderName, nil
}

// GetLeader returns the current leader name
func (le *LeaderElection) GetLeader(ctx context.Context) (string, error) {
	resp, err := le.election.Leader(ctx)
	if err != nil {
		if err == concurrency.ErrElectionNoLeader {
			return "", nil
		}
		return "", err
	}

	if len(resp.Kvs) == 0 {
		return "", nil
	}

	return string(resp.Kvs[0].Value), nil
}

// Resign resigns from leadership
func (le *LeaderElection) Resign(ctx context.Context) error {
	le.logger.Info("resigning from leadership",
		"key", le.config.ElectionKey,
		"name", le.config.LeaderName,
	)

	// Call lose leadership callback
	if le.onLoseLeadership != nil {
		if err := le.onLoseLeadership(ctx); err != nil {
			le.logger.Error("lose leadership callback failed", "error", err)
		}
	}

	if err := le.election.Resign(ctx); err != nil {
		return fmt.Errorf("resign failed: %w", err)
	}

	return nil
}

// Close closes the leader election instance
func (le *LeaderElection) Close() error {
	if le.session != nil {
		if err := le.session.Close(); err != nil {
			le.logger.Error("failed to close session", "error", err)
		}
	}

	if le.client != nil {
		return le.client.Close()
	}

	return nil
}

// LeadershipManager manages the leader election lifecycle
type LeadershipManager struct {
	logger   *slog.Logger
	election *LeaderElection
	config   ElectionConfig
}

// NewLeadershipManager creates a new leadership manager
func NewLeadershipManager(logger *slog.Logger, config ElectionConfig) (*LeadershipManager, error) {
	election, err := NewLeaderElection(logger, config)
	if err != nil {
		return nil, err
	}

	return &LeadershipManager{
		logger:   logger,
		election: election,
		config:   config,
	}, nil
}

// Run starts the leadership manager and handles leader election
func (lm *LeadershipManager) Run(ctx context.Context) error {
	// Set up callbacks
	lm.election.OnBecomeLeader(func(ctx context.Context) error {
		lm.logger.Info("this node is now the leader")
		// Leader initialization logic here
		return nil
	})

	lm.election.OnLoseLeadership(func(ctx context.Context) error {
		lm.logger.Info("this node lost leadership")
		// Leader cleanup logic here
		return nil
	})

	// Start observing leader changes
	observeCh, err := lm.election.Observe(ctx)
	if err != nil {
		return fmt.Errorf("failed to start observer: %w", err)
	}

	// Campaign for leadership
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			default:
				if err := lm.election.Campaign(ctx); err != nil {
					if ctx.Err() != nil {
						return
					}
					lm.logger.Error("campaign failed, retrying", "error", err)
					time.Sleep(5 * time.Second)
				}
			}
		}
	}()

	// Watch for leader changes
	for {
		select {
		case <-ctx.Done():
			lm.logger.Info("shutting down leadership manager")
			return lm.election.Close()
		case leader, ok := <-observeCh:
			if !ok {
				lm.logger.Info("observer channel closed")
				return nil
			}
			lm.logger.Info("observed leader change", "leader", leader)
		}
	}
}

// IsLeader returns true if this node is the leader
func (lm *LeadershipManager) IsLeader(ctx context.Context) (bool, error) {
	return lm.election.IsLeader(ctx)
}

// GetLeader returns the current leader
func (lm *LeadershipManager) GetLeader(ctx context.Context) (string, error) {
	return lm.election.GetLeader(ctx)
}

// Resign resigns from leadership
func (lm *LeadershipManager) Resign(ctx context.Context) error {
	return lm.election.Resign(ctx)
}
