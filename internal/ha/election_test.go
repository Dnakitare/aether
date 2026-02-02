package ha_test

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/aether-runtime/aether/internal/ha"
)

func TestElectionConfig(t *testing.T) {
	config := ha.DefaultElectionConfig()

	if len(config.EtcdEndpoints) == 0 {
		t.Error("Expected EtcdEndpoints to be set")
	}

	if config.ElectionKey == "" {
		t.Error("Expected ElectionKey to be set")
	}

	if config.SessionTTL == 0 {
		t.Error("Expected SessionTTL to be set")
	}

	if config.LeaderName == "" {
		t.Error("Expected LeaderName to be set")
	}
}

func TestLeaderElection(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping etcd integration test in short mode")
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	config := ha.DefaultElectionConfig()
	config.LeaderName = "test-node-1"

	election, err := ha.NewLeaderElection(logger, config)
	if err != nil {
		t.Skipf("etcd not available: %v", err)
	}
	defer election.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Test campaign
	becameLeader := false
	election.OnBecomeLeader(func(ctx context.Context) error {
		becameLeader = true
		return nil
	})

	go func() {
		if err := election.Campaign(ctx); err != nil {
			t.Logf("Campaign error: %v", err)
		}
	}()

	// Give it time to become leader
	time.Sleep(2 * time.Second)

	// Test IsLeader
	isLeader, err := election.IsLeader(ctx)
	if err != nil {
		t.Fatalf("Failed to check leader status: %v", err)
	}

	if !isLeader {
		t.Error("Expected to be leader")
	}

	if !becameLeader {
		t.Error("OnBecomeLeader callback not called")
	}

	// Test GetLeader
	leader, err := election.GetLeader(ctx)
	if err != nil {
		t.Fatalf("Failed to get leader: %v", err)
	}

	if leader != config.LeaderName {
		t.Errorf("Expected leader = %s, got %s", config.LeaderName, leader)
	}
}

func TestLeaderElectionMultipleNodes(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping etcd integration test in short mode")
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Create two nodes
	config1 := ha.DefaultElectionConfig()
	config1.LeaderName = "test-node-1"
	config1.ElectionKey = "/aether/test-election"

	config2 := ha.DefaultElectionConfig()
	config2.LeaderName = "test-node-2"
	config2.ElectionKey = "/aether/test-election"

	election1, err := ha.NewLeaderElection(logger, config1)
	if err != nil {
		t.Skipf("etcd not available: %v", err)
	}
	defer election1.Close()

	election2, err := ha.NewLeaderElection(logger, config2)
	if err != nil {
		t.Fatalf("Failed to create second election: %v", err)
	}
	defer election2.Close()

	// Start campaigns
	go election1.Campaign(ctx)
	go election2.Campaign(ctx)

	// Wait for election to complete
	time.Sleep(2 * time.Second)

	// One should be leader, one should not
	isLeader1, _ := election1.IsLeader(ctx)
	isLeader2, _ := election2.IsLeader(ctx)

	if isLeader1 == isLeader2 {
		t.Error("Expected exactly one leader")
	}
}

func TestLeadershipManager(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping etcd integration test in short mode")
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	config := ha.DefaultElectionConfig()
	config.LeaderName = "test-manager-node"
	config.ElectionKey = "/aether/test-manager"

	manager, err := ha.NewLeadershipManager(logger, config)
	if err != nil {
		t.Skipf("etcd not available: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go func() {
		if err := manager.Run(ctx); err != nil {
			t.Logf("Manager run error: %v", err)
		}
	}()

	// Give it time to become leader
	time.Sleep(2 * time.Second)

	isLeader, err := manager.IsLeader(ctx)
	if err != nil {
		t.Fatalf("Failed to check leader status: %v", err)
	}

	if !isLeader {
		t.Error("Expected manager to be leader")
	}

	leader, err := manager.GetLeader(ctx)
	if err != nil {
		t.Fatalf("Failed to get leader: %v", err)
	}

	if leader != config.LeaderName {
		t.Errorf("Expected leader = %s, got %s", config.LeaderName, leader)
	}
}

func TestReplicationConfig(t *testing.T) {
	config := ha.DefaultReplicationConfig()

	if len(config.EtcdEndpoints) == 0 {
		t.Error("Expected EtcdEndpoints to be set")
	}

	if config.StatePrefix == "" {
		t.Error("Expected StatePrefix to be set")
	}

	if config.SyncInterval == 0 {
		t.Error("Expected SyncInterval to be set")
	}

	if config.LagThreshold == 0 {
		t.Error("Expected LagThreshold to be set")
	}
}

func TestStateReplication(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping etcd integration test in short mode")
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	config := ha.DefaultReplicationConfig()
	config.StatePrefix = "/aether/test-state/"

	replication, err := ha.NewStateReplication(logger, config)
	if err != nil {
		t.Skipf("etcd not available: %v", err)
	}
	defer replication.Close()

	ctx := context.Background()

	// Test Put and Get
	testKey := "test-key"
	testValue := map[string]interface{}{
		"foo": "bar",
		"baz": 123,
	}

	err = replication.Put(ctx, testKey, testValue)
	if err != nil {
		t.Fatalf("Failed to put state: %v", err)
	}

	value, err := replication.Get(ctx, testKey)
	if err != nil {
		t.Fatalf("Failed to get state: %v", err)
	}

	if value == nil {
		t.Error("Expected non-nil value")
	}

	// Test Delete
	err = replication.Delete(ctx, testKey)
	if err != nil {
		t.Fatalf("Failed to delete state: %v", err)
	}

	_, err = replication.Get(ctx, testKey)
	if err == nil {
		t.Error("Expected error when getting deleted key")
	}
}

func TestFailoverConfig(t *testing.T) {
	config := ha.DefaultFailoverConfig()

	if config.HealthCheckInterval == 0 {
		t.Error("Expected HealthCheckInterval to be set")
	}

	if config.HealthCheckTimeout == 0 {
		t.Error("Expected HealthCheckTimeout to be set")
	}

	if config.FailureThreshold == 0 {
		t.Error("Expected FailureThreshold to be set")
	}

	if config.CooldownPeriod == 0 {
		t.Error("Expected CooldownPeriod to be set")
	}

	if config.MaxFailovers == 0 {
		t.Error("Expected MaxFailovers to be set")
	}
}

func TestClusterConfig(t *testing.T) {
	nodeName := "test-node"
	config := ha.DefaultClusterConfig(nodeName)

	if config.NodeName != nodeName {
		t.Errorf("Expected NodeName = %s, got %s", nodeName, config.NodeName)
	}

	if config.ElectionConfig.LeaderName != nodeName {
		t.Errorf("Expected LeaderName = %s, got %s", nodeName, config.ElectionConfig.LeaderName)
	}
}
