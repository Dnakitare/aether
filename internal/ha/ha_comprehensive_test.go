package ha_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aether-runtime/aether/internal/ha"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test helpers

func setupTestLogger(t *testing.T) *slog.Logger {
	t.Helper()
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func skipIfNoEtcd(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Skipf("etcd not available: %v", err)
	}
}

// Test Category 1: Leader Election Tests (10 test cases)

func TestLeaderElectionBasics(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping HA tests in short mode (requires etcd)")
	}

	t.Run("single_node_becomes_leader", func(t *testing.T) {
		logger := setupTestLogger(t)
		config := ha.DefaultElectionConfig()
		config.LeaderName = "node-1"
		config.ElectionKey = "/aether/test/single-leader"

		election, err := ha.NewLeaderElection(logger, config)
		skipIfNoEtcd(t, err)
		defer election.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		// Campaign for leadership
		becameLeader := false
		election.OnBecomeLeader(func(ctx context.Context) error {
			becameLeader = true
			return nil
		})

		go election.Campaign(ctx)

		// Wait for election
		time.Sleep(1 * time.Second)

		// Verify leader status
		isLeader, err := election.IsLeader(ctx)
		require.NoError(t, err)
		assert.True(t, isLeader)
		assert.True(t, becameLeader)

		// Verify GetLeader returns correct leader
		leader, err := election.GetLeader(ctx)
		require.NoError(t, err)
		assert.Equal(t, "node-1", leader)
	})

	t.Run("resign_from_leadership", func(t *testing.T) {
		logger := setupTestLogger(t)
		config := ha.DefaultElectionConfig()
		config.LeaderName = "node-resign"
		config.ElectionKey = "/aether/test/resign"

		election, err := ha.NewLeaderElection(logger, config)
		skipIfNoEtcd(t, err)
		defer election.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		// Become leader
		lostLeadership := false
		election.OnLoseLeadership(func(ctx context.Context) error {
			lostLeadership = true
			return nil
		})

		go election.Campaign(ctx)
		time.Sleep(1 * time.Second)

		// Verify is leader
		isLeader, err := election.IsLeader(ctx)
		require.NoError(t, err)
		assert.True(t, isLeader)

		// Resign
		err = election.Resign(ctx)
		require.NoError(t, err)
		assert.True(t, lostLeadership)

		time.Sleep(500 * time.Millisecond)

		// Verify no longer leader
		isLeader, err = election.IsLeader(ctx)
		require.NoError(t, err)
		assert.False(t, isLeader)
	})

	t.Run("multiple_nodes_single_leader", func(t *testing.T) {
		logger := setupTestLogger(t)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		// Create 3 nodes
		nodes := make([]*ha.LeaderElection, 3)
		leaderCounts := make([]int32, 3)

		for i := 0; i < 3; i++ {
			config := ha.DefaultElectionConfig()
			config.LeaderName = fmt.Sprintf("node-%d", i)
			config.ElectionKey = "/aether/test/multi-leader"

			election, err := ha.NewLeaderElection(logger, config)
			skipIfNoEtcd(t, err)
			defer election.Close()

			nodes[i] = election

			idx := i
			election.OnBecomeLeader(func(ctx context.Context) error {
				atomic.AddInt32(&leaderCounts[idx], 1)
				return nil
			})

			go election.Campaign(ctx)
		}

		// Wait for election to complete
		time.Sleep(2 * time.Second)

		// Verify exactly one leader
		leaderCount := 0
		var leaderName string

		for i, node := range nodes {
			isLeader, err := node.IsLeader(ctx)
			require.NoError(t, err)

			if isLeader {
				leaderCount++
				leaderName = fmt.Sprintf("node-%d", i)
			}
		}

		assert.Equal(t, 1, leaderCount, "Expected exactly one leader")
		assert.NotEmpty(t, leaderName)

		// Verify all nodes see the same leader
		for _, node := range nodes {
			leader, err := node.GetLeader(ctx)
			require.NoError(t, err)
			assert.Equal(t, leaderName, leader)
		}
	})

	t.Run("leader_failure_triggers_new_election", func(t *testing.T) {
		logger := setupTestLogger(t)
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		// Create node 1 (initial leader)
		config1 := ha.DefaultElectionConfig()
		config1.LeaderName = "node-1"
		config1.ElectionKey = "/aether/test/failover"
		config1.SessionTTL = 2

		election1, err := ha.NewLeaderElection(logger, config1)
		skipIfNoEtcd(t, err)

		go election1.Campaign(ctx)
		time.Sleep(1 * time.Second)

		// Verify node 1 is leader
		isLeader, err := election1.IsLeader(ctx)
		require.NoError(t, err)
		assert.True(t, isLeader)

		// Create node 2 (standby)
		config2 := ha.DefaultElectionConfig()
		config2.LeaderName = "node-2"
		config2.ElectionKey = "/aether/test/failover"

		election2, err := ha.NewLeaderElection(logger, config2)
		skipIfNoEtcd(t, err)
		defer election2.Close()

		becameLeader := false
		election2.OnBecomeLeader(func(ctx context.Context) error {
			becameLeader = true
			return nil
		})

		go election2.Campaign(ctx)
		time.Sleep(1 * time.Second)

		// Close node 1 (simulate failure)
		election1.Close()

		// Wait for session TTL + election time
		time.Sleep(4 * time.Second)

		// Verify node 2 became leader
		isLeader, err = election2.IsLeader(ctx)
		require.NoError(t, err)
		assert.True(t, isLeader)
		assert.True(t, becameLeader)
	})

	t.Run("observe_leader_changes", func(t *testing.T) {
		logger := setupTestLogger(t)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		config := ha.DefaultElectionConfig()
		config.LeaderName = "observer-node"
		config.ElectionKey = "/aether/test/observe"

		election, err := ha.NewLeaderElection(logger, config)
		skipIfNoEtcd(t, err)
		defer election.Close()

		// Start observing
		observeCh, err := election.Observe(ctx)
		require.NoError(t, err)

		// Campaign
		go election.Campaign(ctx)

		// Wait for leader change notification
		select {
		case leader := <-observeCh:
			assert.Equal(t, "observer-node", leader)
		case <-time.After(5 * time.Second):
			t.Fatal("Timeout waiting for leader change notification")
		}
	})
}

func TestLeaderElectionCallbacks(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping HA tests in short mode (requires etcd)")
	}

	t.Run("become_leader_callback_error", func(t *testing.T) {
		logger := setupTestLogger(t)
		config := ha.DefaultElectionConfig()
		config.LeaderName = "node-error"
		config.ElectionKey = "/aether/test/callback-error"

		election, err := ha.NewLeaderElection(logger, config)
		skipIfNoEtcd(t, err)
		defer election.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		// Set callback that returns error
		election.OnBecomeLeader(func(ctx context.Context) error {
			return errors.New("callback failed")
		})

		// Campaign should fail due to callback error
		err = election.Campaign(ctx)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "callback failed")
	})

	t.Run("lose_leadership_callback_called", func(t *testing.T) {
		logger := setupTestLogger(t)
		config := ha.DefaultElectionConfig()
		config.LeaderName = "node-lose"
		config.ElectionKey = "/aether/test/lose-leadership"

		election, err := ha.NewLeaderElection(logger, config)
		skipIfNoEtcd(t, err)
		defer election.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		callbackCalled := false
		election.OnLoseLeadership(func(ctx context.Context) error {
			callbackCalled = true
			return errors.New("expected error")
		})

		go election.Campaign(ctx)
		time.Sleep(1 * time.Second)

		// Resign should trigger callback even if it returns error
		err = election.Resign(ctx)
		require.NoError(t, err) // Resign succeeds even if callback fails
		assert.True(t, callbackCalled)
	})
}

func TestLeaderElectionEdgeCases(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping HA tests in short mode (requires etcd)")
	}

	t.Run("campaign_with_canceled_context", func(t *testing.T) {
		logger := setupTestLogger(t)
		config := ha.DefaultElectionConfig()
		config.LeaderName = "node-cancel"
		config.ElectionKey = "/aether/test/cancel"

		election, err := ha.NewLeaderElection(logger, config)
		skipIfNoEtcd(t, err)
		defer election.Close()

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		err = election.Campaign(ctx)
		assert.Error(t, err)
	})

	t.Run("is_leader_with_no_leader", func(t *testing.T) {
		logger := setupTestLogger(t)
		config := ha.DefaultElectionConfig()
		config.LeaderName = "node-check"
		config.ElectionKey = "/aether/test/no-leader"

		election, err := ha.NewLeaderElection(logger, config)
		skipIfNoEtcd(t, err)
		defer election.Close()

		ctx := context.Background()

		// Check before campaigning
		isLeader, err := election.IsLeader(ctx)
		require.NoError(t, err)
		assert.False(t, isLeader)

		// GetLeader should return empty
		leader, err := election.GetLeader(ctx)
		require.NoError(t, err)
		assert.Empty(t, leader)
	})

	t.Run("close_without_campaign", func(t *testing.T) {
		logger := setupTestLogger(t)
		config := ha.DefaultElectionConfig()

		election, err := ha.NewLeaderElection(logger, config)
		skipIfNoEtcd(t, err)

		// Close without campaigning should not error
		err = election.Close()
		assert.NoError(t, err)
	})
}

// Test Category 2: State Replication Tests (8 test cases)

func TestStateReplicationBasics(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping HA tests in short mode (requires etcd)")
	}

	t.Run("put_and_get", func(t *testing.T) {
		logger := setupTestLogger(t)
		config := ha.DefaultReplicationConfig()
		config.StatePrefix = "/aether/test/repl/"

		repl, err := ha.NewStateReplication(logger, config)
		skipIfNoEtcd(t, err)
		defer repl.Close()

		ctx := context.Background()

		// Put data
		testData := map[string]interface{}{
			"key1": "value1",
			"key2": 123,
			"key3": []string{"a", "b", "c"},
		}

		err = repl.Put(ctx, "test-key", testData)
		require.NoError(t, err)

		// Get data
		value, err := repl.Get(ctx, "test-key")
		require.NoError(t, err)
		assert.NotNil(t, value)

		// Verify cached retrieval (should be fast)
		value2, err := repl.Get(ctx, "test-key")
		require.NoError(t, err)
		assert.NotNil(t, value2)
	})

	t.Run("delete_key", func(t *testing.T) {
		logger := setupTestLogger(t)
		config := ha.DefaultReplicationConfig()
		config.StatePrefix = "/aether/test/delete/"

		repl, err := ha.NewStateReplication(logger, config)
		skipIfNoEtcd(t, err)
		defer repl.Close()

		ctx := context.Background()

		// Put then delete
		err = repl.Put(ctx, "delete-me", "value")
		require.NoError(t, err)

		err = repl.Delete(ctx, "delete-me")
		require.NoError(t, err)

		// Get should fail
		_, err = repl.Get(ctx, "delete-me")
		assert.Error(t, err)
	})

	t.Run("list_keys", func(t *testing.T) {
		logger := setupTestLogger(t)
		config := ha.DefaultReplicationConfig()
		config.StatePrefix = "/aether/test/list/"

		repl, err := ha.NewStateReplication(logger, config)
		skipIfNoEtcd(t, err)
		defer repl.Close()

		ctx := context.Background()

		// Put multiple keys
		for i := 0; i < 5; i++ {
			err = repl.Put(ctx, fmt.Sprintf("key-%d", i), i)
			require.NoError(t, err)
		}

		// List keys
		keys, err := repl.List(ctx, "")
		require.NoError(t, err)
		assert.Len(t, keys, 5)
	})

	t.Run("watch_for_changes", func(t *testing.T) {
		logger := setupTestLogger(t)
		config := ha.DefaultReplicationConfig()
		config.StatePrefix = "/aether/test/watch/"

		repl, err := ha.NewStateReplication(logger, config)
		skipIfNoEtcd(t, err)
		defer repl.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		// Start watching
		watchCh := repl.Watch(ctx, "watch-key")

		// Put data in another goroutine
		go func() {
			time.Sleep(500 * time.Millisecond)
			repl.Put(context.Background(), "watch-key", "watched-value")
		}()

		// Wait for watch event
		select {
		case state := <-watchCh:
			assert.Equal(t, "watch-key", state.Key)
			assert.NotNil(t, state.Value)
		case <-time.After(3 * time.Second):
			t.Fatal("Timeout waiting for watch event")
		}
	})
}

func TestStateReplicationSync(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping HA tests in short mode (requires etcd)")
	}

	t.Run("manual_sync", func(t *testing.T) {
		logger := setupTestLogger(t)
		config := ha.DefaultReplicationConfig()
		config.StatePrefix = "/aether/test/sync/"

		repl, err := ha.NewStateReplication(logger, config)
		skipIfNoEtcd(t, err)
		defer repl.Close()

		ctx := context.Background()

		// Put some data
		for i := 0; i < 3; i++ {
			err = repl.Put(ctx, fmt.Sprintf("sync-key-%d", i), i)
			require.NoError(t, err)
		}

		// Sync
		err = repl.Sync(ctx)
		require.NoError(t, err)

		// Verify all keys are in local cache
		for i := 0; i < 3; i++ {
			value, err := repl.Get(ctx, fmt.Sprintf("sync-key-%d", i))
			require.NoError(t, err)
			assert.NotNil(t, value)
		}
	})

	t.Run("auto_sync", func(t *testing.T) {
		logger := setupTestLogger(t)
		config := ha.DefaultReplicationConfig()
		config.StatePrefix = "/aether/test/autosync/"
		config.SyncInterval = 500 * time.Millisecond

		repl, err := ha.NewStateReplication(logger, config)
		skipIfNoEtcd(t, err)
		defer repl.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		// Start auto sync
		go repl.StartAutoSync(ctx)

		// Put data
		err = repl.Put(context.Background(), "auto-sync-key", "auto-value")
		require.NoError(t, err)

		// Wait for auto sync
		time.Sleep(1 * time.Second)

		// Should be synced
		value, err := repl.Get(context.Background(), "auto-sync-key")
		require.NoError(t, err)
		assert.NotNil(t, value)
	})
}

func TestStateReplicationSnapshot(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping HA tests in short mode (requires etcd)")
	}

	t.Run("create_and_restore_snapshot", func(t *testing.T) {
		logger := setupTestLogger(t)
		config := ha.DefaultReplicationConfig()
		config.StatePrefix = "/aether/test/snapshot/"

		repl, err := ha.NewStateReplication(logger, config)
		skipIfNoEtcd(t, err)
		defer repl.Close()

		ctx := context.Background()

		// Put test data
		testData := map[string]interface{}{
			"key1": "value1",
			"key2": 123,
		}

		for k, v := range testData {
			err = repl.Put(ctx, k, v)
			require.NoError(t, err)
		}

		// Create snapshot
		snapshot, err := repl.CreateSnapshot(ctx)
		require.NoError(t, err)
		assert.NotNil(t, snapshot)
		assert.Len(t, snapshot.State, 2)
		assert.NotZero(t, snapshot.Version)

		// Delete all data
		for k := range testData {
			err = repl.Delete(ctx, k)
			require.NoError(t, err)
		}

		// Verify deleted
		keys, err := repl.List(ctx, "")
		require.NoError(t, err)
		assert.Empty(t, keys)

		// Restore snapshot
		err = repl.RestoreSnapshot(ctx, snapshot)
		require.NoError(t, err)

		// Verify restored
		keys, err = repl.List(ctx, "")
		require.NoError(t, err)
		assert.Len(t, keys, 2)
	})

	t.Run("snapshot_with_empty_state", func(t *testing.T) {
		logger := setupTestLogger(t)
		config := ha.DefaultReplicationConfig()
		config.StatePrefix = "/aether/test/empty-snapshot/"

		repl, err := ha.NewStateReplication(logger, config)
		skipIfNoEtcd(t, err)
		defer repl.Close()

		ctx := context.Background()

		// Create snapshot with no data
		snapshot, err := repl.CreateSnapshot(ctx)
		require.NoError(t, err)
		assert.NotNil(t, snapshot)
		assert.Empty(t, snapshot.State)
	})
}

// Test Category 3: Failover Manager Tests (7 test cases)

func TestFailoverManagerBasics(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping HA tests in short mode (requires etcd)")
	}

	t.Run("register_health_check", func(t *testing.T) {
		logger := setupTestLogger(t)
		config := ha.DefaultFailoverConfig()

		fm := ha.NewFailoverManager(logger, nil, nil, config)

		// Register health check
		check := &ha.HealthCheck{
			Name: "test-service",
			Check: func(ctx context.Context) error {
				return nil
			},
		}

		fm.RegisterHealthCheck(check)

		// Verify registered
		status, ok := fm.GetHealthStatus("test-service")
		assert.True(t, ok)
		assert.Equal(t, "test-service", status.Service)
		assert.True(t, status.Healthy)
	})

	t.Run("health_check_passes", func(t *testing.T) {
		logger := setupTestLogger(t)
		config := ha.DefaultFailoverConfig()
		config.HealthCheckInterval = 500 * time.Millisecond

		fm := ha.NewFailoverManager(logger, nil, nil, config)

		checkCount := int32(0)
		check := &ha.HealthCheck{
			Name: "passing-service",
			Check: func(ctx context.Context) error {
				atomic.AddInt32(&checkCount, 1)
				return nil
			},
			Interval: 500 * time.Millisecond,
		}

		fm.RegisterHealthCheck(check)

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		// Start failover manager
		go fm.Start(ctx)

		// Wait for checks to run
		time.Sleep(1500 * time.Millisecond)

		// Verify health checks ran
		assert.True(t, atomic.LoadInt32(&checkCount) >= 2)

		// Verify service is healthy
		assert.True(t, fm.IsHealthy("passing-service"))
	})

	t.Run("health_check_fails_triggers_failover", func(t *testing.T) {
		logger := setupTestLogger(t)
		config := ha.DefaultFailoverConfig()
		config.HealthCheckInterval = 200 * time.Millisecond
		config.FailureThreshold = 2

		fm := ha.NewFailoverManager(logger, nil, nil, config)

		failoverCalled := int32(0)
		fm.OnFailover(func(service string) error {
			atomic.StoreInt32(&failoverCalled, 1)
			assert.Equal(t, "failing-service", service)
			return nil
		})

		checkCount := int32(0)
		check := &ha.HealthCheck{
			Name: "failing-service",
			Check: func(ctx context.Context) error {
				atomic.AddInt32(&checkCount, 1)
				return errors.New("service unhealthy")
			},
			Interval: 200 * time.Millisecond,
		}

		fm.RegisterHealthCheck(check)

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		go fm.Start(ctx)

		// Wait for failover to trigger
		time.Sleep(1 * time.Second)

		// Verify failover was triggered
		assert.Equal(t, int32(1), atomic.LoadInt32(&failoverCalled))
		assert.False(t, fm.IsHealthy("failing-service"))

		status, ok := fm.GetHealthStatus("failing-service")
		assert.True(t, ok)
		assert.False(t, status.Healthy)
		assert.True(t, status.ConsecutiveFails >= config.FailureThreshold)
	})

	t.Run("service_recovery_after_failure", func(t *testing.T) {
		logger := setupTestLogger(t)
		config := ha.DefaultFailoverConfig()
		config.HealthCheckInterval = 200 * time.Millisecond
		config.FailureThreshold = 2

		fm := ha.NewFailoverManager(logger, nil, nil, config)

		// Service fails first 3 times, then recovers
		checkCount := int32(0)
		check := &ha.HealthCheck{
			Name: "recovering-service",
			Check: func(ctx context.Context) error {
				count := atomic.AddInt32(&checkCount, 1)
				if count <= 3 {
					return errors.New("temporarily unhealthy")
				}
				return nil
			},
			Interval: 200 * time.Millisecond,
		}

		fm.RegisterHealthCheck(check)

		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		go fm.Start(ctx)

		// Wait for initial failures
		time.Sleep(800 * time.Millisecond)
		assert.False(t, fm.IsHealthy("recovering-service"))

		// Wait for recovery
		time.Sleep(1 * time.Second)
		assert.True(t, fm.IsHealthy("recovering-service"))

		status, _ := fm.GetHealthStatus("recovering-service")
		assert.Equal(t, 0, status.ConsecutiveFails)
	})
}

func TestFailoverManagerCooldown(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping HA tests in short mode (requires etcd)")
	}

	t.Run("max_failovers_in_cooldown_period", func(t *testing.T) {
		logger := setupTestLogger(t)
		config := ha.DefaultFailoverConfig()
		config.HealthCheckInterval = 100 * time.Millisecond
		config.FailureThreshold = 1
		config.CooldownPeriod = 2 * time.Second
		config.MaxFailovers = 2

		fm := ha.NewFailoverManager(logger, nil, nil, config)

		failoverCount := int32(0)
		fm.OnFailover(func(service string) error {
			atomic.AddInt32(&failoverCount, 1)
			return nil
		})

		check := &ha.HealthCheck{
			Name: "flapping-service",
			Check: func(ctx context.Context) error {
				return errors.New("always failing")
			},
			Interval: 100 * time.Millisecond,
		}

		fm.RegisterHealthCheck(check)

		ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
		defer cancel()

		go fm.Start(ctx)

		// Wait for cooldown period
		time.Sleep(1400 * time.Millisecond)

		// Should hit max failovers (2)
		count := atomic.LoadInt32(&failoverCount)
		assert.LessOrEqual(t, count, int32(2))

		status, _ := fm.GetHealthStatus("flapping-service")
		assert.LessOrEqual(t, status.FailoverCount, 2)
	})

	t.Run("force_failover", func(t *testing.T) {
		logger := setupTestLogger(t)
		config := ha.DefaultFailoverConfig()

		fm := ha.NewFailoverManager(logger, nil, nil, config)

		forcedFailover := false
		fm.OnFailover(func(service string) error {
			forcedFailover = true
			return nil
		})

		check := &ha.HealthCheck{
			Name: "manual-service",
			Check: func(ctx context.Context) error {
				return nil // Always healthy
			},
		}

		fm.RegisterHealthCheck(check)

		ctx := context.Background()

		// Force failover manually
		err := fm.ForceFailover(ctx, "manual-service")
		require.NoError(t, err)
		assert.True(t, forcedFailover)
	})

	t.Run("force_failover_nonexistent_service", func(t *testing.T) {
		logger := setupTestLogger(t)
		config := ha.DefaultFailoverConfig()

		fm := ha.NewFailoverManager(logger, nil, nil, config)

		ctx := context.Background()

		err := fm.ForceFailover(ctx, "nonexistent")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})
}

// Test Category 4: HA Cluster Integration Tests (5 test cases)

func TestHAClusterIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping HA tests in short mode (requires etcd)")
	}

	t.Run("cluster_initialization", func(t *testing.T) {
		logger := setupTestLogger(t)
		config := ha.DefaultClusterConfig("test-node-1")
		config.ElectionConfig.ElectionKey = "/aether/test/cluster-init"

		cluster, err := ha.NewHACluster(logger, config)
		skipIfNoEtcd(t, err)
		defer cluster.Close()

		assert.NotNil(t, cluster)
	})

	t.Run("cluster_leader_election", func(t *testing.T) {
		logger := setupTestLogger(t)
		config := ha.DefaultClusterConfig("test-cluster-node")
		config.ElectionConfig.ElectionKey = "/aether/test/cluster-leader"

		cluster, err := ha.NewHACluster(logger, config)
		skipIfNoEtcd(t, err)
		defer cluster.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		// Start cluster in background
		go cluster.Start(ctx)

		// Wait for leadership
		time.Sleep(2 * time.Second)

		// Check leader status
		isLeader, err := cluster.IsLeader(ctx)
		require.NoError(t, err)
		assert.True(t, isLeader)

		leader, err := cluster.GetLeader(ctx)
		require.NoError(t, err)
		assert.Equal(t, "test-cluster-node", leader)
	})

	t.Run("cluster_state_replication", func(t *testing.T) {
		logger := setupTestLogger(t)
		config := ha.DefaultClusterConfig("repl-node")
		config.ReplicationConfig.StatePrefix = "/aether/test/cluster-state/"

		cluster, err := ha.NewHACluster(logger, config)
		skipIfNoEtcd(t, err)
		defer cluster.Close()

		ctx := context.Background()

		// Put state
		err = cluster.PutState(ctx, "test-key", map[string]string{"foo": "bar"})
		require.NoError(t, err)

		// Get state
		value, err := cluster.GetState(ctx, "test-key")
		require.NoError(t, err)
		assert.NotNil(t, value)
	})

	t.Run("cluster_health_checks", func(t *testing.T) {
		logger := setupTestLogger(t)
		config := ha.DefaultClusterConfig("health-node")
		config.ElectionConfig.ElectionKey = "/aether/test/cluster-health"

		cluster, err := ha.NewHACluster(logger, config)
		skipIfNoEtcd(t, err)
		defer cluster.Close()

		// Register health check
		check := &ha.HealthCheck{
			Name: "cluster-service",
			Check: func(ctx context.Context) error {
				return nil
			},
		}

		cluster.RegisterHealthCheck(check)

		// Get health status
		status, ok := cluster.GetHealthStatus("cluster-service")
		assert.True(t, ok)
		assert.True(t, status.Healthy)
	})

	t.Run("multi_cluster_coordination", func(t *testing.T) {
		logger := setupTestLogger(t)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		// Create 2 cluster nodes
		config1 := ha.DefaultClusterConfig("cluster-node-1")
		config1.ElectionConfig.ElectionKey = "/aether/test/multi-cluster"
		config1.ReplicationConfig.StatePrefix = "/aether/test/multi-state/"

		cluster1, err := ha.NewHACluster(logger, config1)
		skipIfNoEtcd(t, err)
		defer cluster1.Close()

		config2 := ha.DefaultClusterConfig("cluster-node-2")
		config2.ElectionConfig.ElectionKey = "/aether/test/multi-cluster"
		config2.ReplicationConfig.StatePrefix = "/aether/test/multi-state/"

		cluster2, err := ha.NewHACluster(logger, config2)
		skipIfNoEtcd(t, err)
		defer cluster2.Close()

		// Start both clusters
		go cluster1.Start(ctx)
		go cluster2.Start(ctx)

		// Wait for election
		time.Sleep(2 * time.Second)

		// One should be leader
		isLeader1, _ := cluster1.IsLeader(ctx)
		isLeader2, _ := cluster2.IsLeader(ctx)

		assert.NotEqual(t, isLeader1, isLeader2, "Exactly one should be leader")

		// Put state on leader
		var leaderCluster *ha.HACluster
		if isLeader1 {
			leaderCluster = cluster1
		} else {
			leaderCluster = cluster2
		}

		err = leaderCluster.PutState(ctx, "shared-key", "shared-value")
		require.NoError(t, err)

		// Both nodes should see the state
		time.Sleep(500 * time.Millisecond)

		value1, err := cluster1.GetState(ctx, "shared-key")
		require.NoError(t, err)
		assert.NotNil(t, value1)

		value2, err := cluster2.GetState(ctx, "shared-key")
		require.NoError(t, err)
		assert.NotNil(t, value2)
	})
}

// Test Category 5: Concurrent Operations Tests (3 test cases)

func TestConcurrentOperations(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping HA tests in short mode (requires etcd)")
	}

	t.Run("concurrent_state_writes", func(t *testing.T) {
		logger := setupTestLogger(t)
		config := ha.DefaultReplicationConfig()
		config.StatePrefix = "/aether/test/concurrent-write/"

		repl, err := ha.NewStateReplication(logger, config)
		skipIfNoEtcd(t, err)
		defer repl.Close()

		ctx := context.Background()

		// Write 100 keys concurrently
		const numWrites = 100
		var wg sync.WaitGroup
		errors := make([]error, numWrites)

		for i := 0; i < numWrites; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				errors[idx] = repl.Put(ctx, fmt.Sprintf("key-%d", idx), idx)
			}(i)
		}

		wg.Wait()

		// Verify no errors
		for i, err := range errors {
			assert.NoError(t, err, "Write %d failed", i)
		}

		// Verify all keys exist
		keys, err := repl.List(ctx, "")
		require.NoError(t, err)
		assert.Len(t, keys, numWrites)
	})

	t.Run("concurrent_health_checks", func(t *testing.T) {
		logger := setupTestLogger(t)
		config := ha.DefaultFailoverConfig()
		config.HealthCheckInterval = 100 * time.Millisecond

		fm := ha.NewFailoverManager(logger, nil, nil, config)

		// Register 10 services
		const numServices = 10
		for i := 0; i < numServices; i++ {
			serviceName := fmt.Sprintf("service-%d", i)
			check := &ha.HealthCheck{
				Name: serviceName,
				Check: func(ctx context.Context) error {
					return nil
				},
				Interval: 100 * time.Millisecond,
			}
			fm.RegisterHealthCheck(check)
		}

		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()

		// Start failover manager
		go fm.Start(ctx)

		// Wait for checks to run
		time.Sleep(500 * time.Millisecond)

		// Verify all services are healthy
		allStatus := fm.GetAllHealthStatus()
		assert.Len(t, allStatus, numServices)

		for i := 0; i < numServices; i++ {
			serviceName := fmt.Sprintf("service-%d", i)
			assert.True(t, fm.IsHealthy(serviceName))
		}
	})

	t.Run("concurrent_failovers", func(t *testing.T) {
		logger := setupTestLogger(t)
		config := ha.DefaultFailoverConfig()

		fm := ha.NewFailoverManager(logger, nil, nil, config)

		failoverCount := int32(0)
		fm.OnFailover(func(service string) error {
			atomic.AddInt32(&failoverCount, 1)
			return nil
		})

		// Register 5 services
		const numServices = 5
		for i := 0; i < numServices; i++ {
			serviceName := fmt.Sprintf("fail-service-%d", i)
			check := &ha.HealthCheck{
				Name: serviceName,
				Check: func(ctx context.Context) error {
					return nil
				},
			}
			fm.RegisterHealthCheck(check)
		}

		ctx := context.Background()

		// Trigger failovers concurrently
		var wg sync.WaitGroup
		for i := 0; i < numServices; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				serviceName := fmt.Sprintf("fail-service-%d", idx)
				fm.ForceFailover(ctx, serviceName)
			}(i)
		}

		wg.Wait()

		// Verify all failovers executed
		assert.Equal(t, int32(numServices), atomic.LoadInt32(&failoverCount))
	})
}
