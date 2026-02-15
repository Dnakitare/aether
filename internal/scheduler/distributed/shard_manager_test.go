package distributed

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"
)

// Skip these tests if etcd is not available
func skipIfNoEtcd(t *testing.T) {
	if os.Getenv("ETCD_AVAILABLE") != "true" {
		t.Skip("Skipping test: etcd not available (set ETCD_AVAILABLE=true to run)")
	}
}

func TestShardManager_RegisterAndDiscover(t *testing.T) {
	skipIfNoEtcd(t)

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	config1 := DefaultShardConfig()
	config1.SchedulerID = "scheduler-1"
	config1.InstanceID = "instance-1"
	config1.Hostname = "host-1"
	config1.VirtualNodes = 10

	config2 := DefaultShardConfig()
	config2.SchedulerID = "scheduler-2"
	config2.InstanceID = "instance-2"
	config2.Hostname = "host-2"
	config2.VirtualNodes = 10

	sm1, err := NewShardManager(logger, config1)
	if err != nil {
		t.Fatalf("Failed to create shard manager 1: %v", err)
	}
	defer sm1.Stop(context.Background())

	sm2, err := NewShardManager(logger, config2)
	if err != nil {
		t.Fatalf("Failed to create shard manager 2: %v", err)
	}
	defer sm2.Stop(context.Background())

	ctx := context.Background()

	// Start both shard managers
	if err := sm1.Start(ctx); err != nil {
		t.Fatalf("Failed to start shard manager 1: %v", err)
	}

	if err := sm2.Start(ctx); err != nil {
		t.Fatalf("Failed to start shard manager 2: %v", err)
	}

	// Give time for discovery
	time.Sleep(2 * time.Second)

	// Verify both see each other
	members1 := sm1.Members()
	if len(members1) != 2 {
		t.Errorf("Shard manager 1 members = %d, want 2", len(members1))
	}

	members2 := sm2.Members()
	if len(members2) != 2 {
		t.Errorf("Shard manager 2 members = %d, want 2", len(members2))
	}

	// Verify scheduler assignment
	nodeID := "node-001"
	scheduler1 := sm1.GetScheduler(nodeID)
	scheduler2 := sm2.GetScheduler(nodeID)

	if scheduler1 != scheduler2 {
		t.Errorf("Inconsistent scheduler assignment: sm1=%s, sm2=%s", scheduler1, scheduler2)
	}

	if scheduler1 != "scheduler-1" && scheduler1 != "scheduler-2" {
		t.Errorf("Invalid scheduler assignment: %s", scheduler1)
	}
}

func TestShardManager_IsOwner(t *testing.T) {
	skipIfNoEtcd(t)

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	config := DefaultShardConfig()
	config.SchedulerID = "scheduler-1"
	config.InstanceID = "instance-1"
	config.Hostname = "host-1"
	config.VirtualNodes = 10

	sm, err := NewShardManager(logger, config)
	if err != nil {
		t.Fatalf("Failed to create shard manager: %v", err)
	}
	defer sm.Stop(context.Background())

	ctx := context.Background()

	if err := sm.Start(ctx); err != nil {
		t.Fatalf("Failed to start shard manager: %v", err)
	}

	// Give time for registration
	time.Sleep(1 * time.Second)

	// With only one scheduler, it owns all keys
	if !sm.IsOwner("node-001") {
		t.Error("IsOwner(node-001) = false, want true (only scheduler)")
	}

	if !sm.IsOwner("node-002") {
		t.Error("IsOwner(node-002) = false, want true (only scheduler)")
	}
}

func TestShardManager_Heartbeat(t *testing.T) {
	skipIfNoEtcd(t)

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	config := DefaultShardConfig()
	config.SchedulerID = "scheduler-1"
	config.InstanceID = "instance-1"
	config.Hostname = "host-1"
	config.HeartbeatInterval = 1 * time.Second
	config.VirtualNodes = 10

	sm, err := NewShardManager(logger, config)
	if err != nil {
		t.Fatalf("Failed to create shard manager: %v", err)
	}
	defer sm.Stop(context.Background())

	ctx := context.Background()

	if err := sm.Start(ctx); err != nil {
		t.Fatalf("Failed to start shard manager: %v", err)
	}

	// Get initial heartbeat
	time.Sleep(500 * time.Millisecond)
	info1, err := sm.GetShardInfo(ctx, "scheduler-1")
	if err != nil {
		t.Fatalf("Failed to get shard info: %v", err)
	}

	// Wait for heartbeat update
	time.Sleep(2 * time.Second)
	info2, err := sm.GetShardInfo(ctx, "scheduler-1")
	if err != nil {
		t.Fatalf("Failed to get shard info after heartbeat: %v", err)
	}

	// Heartbeat should have been updated
	if !info2.Heartbeat.After(info1.Heartbeat) {
		t.Errorf("Heartbeat not updated: before=%v, after=%v", info1.Heartbeat, info2.Heartbeat)
	}
}

func TestShardManager_ListShards(t *testing.T) {
	skipIfNoEtcd(t)

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	config1 := DefaultShardConfig()
	config1.SchedulerID = "scheduler-1"
	config1.InstanceID = "instance-1"
	config1.Hostname = "host-1"
	config1.VirtualNodes = 10

	config2 := DefaultShardConfig()
	config2.SchedulerID = "scheduler-2"
	config2.InstanceID = "instance-2"
	config2.Hostname = "host-2"
	config2.VirtualNodes = 10

	sm1, err := NewShardManager(logger, config1)
	if err != nil {
		t.Fatalf("Failed to create shard manager 1: %v", err)
	}
	defer sm1.Stop(context.Background())

	sm2, err := NewShardManager(logger, config2)
	if err != nil {
		t.Fatalf("Failed to create shard manager 2: %v", err)
	}
	defer sm2.Stop(context.Background())

	ctx := context.Background()

	if err := sm1.Start(ctx); err != nil {
		t.Fatalf("Failed to start shard manager 1: %v", err)
	}

	if err := sm2.Start(ctx); err != nil {
		t.Fatalf("Failed to start shard manager 2: %v", err)
	}

	// Give time for registration
	time.Sleep(1 * time.Second)

	// List all shards
	shards, err := sm1.ListShards(ctx)
	if err != nil {
		t.Fatalf("Failed to list shards: %v", err)
	}

	if len(shards) != 2 {
		t.Errorf("ListShards() = %d shards, want 2", len(shards))
	}

	// Verify shard info
	found := make(map[string]bool)
	for _, shard := range shards {
		found[shard.SchedulerID] = true

		if shard.Status != "active" {
			t.Errorf("Shard %s status = %s, want active", shard.SchedulerID, shard.Status)
		}

		if len(shard.VNodes) != 10 {
			t.Errorf("Shard %s vnodes = %d, want 10", shard.SchedulerID, len(shard.VNodes))
		}
	}

	if !found["scheduler-1"] {
		t.Error("scheduler-1 not in shard list")
	}

	if !found["scheduler-2"] {
		t.Error("scheduler-2 not in shard list")
	}
}

func TestShardManager_OnChanged(t *testing.T) {
	skipIfNoEtcd(t)

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	config1 := DefaultShardConfig()
	config1.SchedulerID = "scheduler-1"
	config1.InstanceID = "instance-1"
	config1.Hostname = "host-1"
	config1.VirtualNodes = 10

	sm1, err := NewShardManager(logger, config1)
	if err != nil {
		t.Fatalf("Failed to create shard manager 1: %v", err)
	}
	defer sm1.Stop(context.Background())

	ctx := context.Background()

	// Set up callback
	changedCh := make(chan []string, 1)
	sm1.OnChanged(func(members []string) {
		changedCh <- members
	})

	if err := sm1.Start(ctx); err != nil {
		t.Fatalf("Failed to start shard manager 1: %v", err)
	}

	// Start second scheduler
	config2 := DefaultShardConfig()
	config2.SchedulerID = "scheduler-2"
	config2.InstanceID = "instance-2"
	config2.Hostname = "host-2"
	config2.VirtualNodes = 10

	sm2, err := NewShardManager(logger, config2)
	if err != nil {
		t.Fatalf("Failed to create shard manager 2: %v", err)
	}
	defer sm2.Stop(context.Background())

	if err := sm2.Start(ctx); err != nil {
		t.Fatalf("Failed to start shard manager 2: %v", err)
	}

	// Wait for callback
	select {
	case members := <-changedCh:
		if len(members) != 2 {
			t.Errorf("OnChanged callback members = %d, want 2", len(members))
		}
	case <-time.After(5 * time.Second):
		t.Error("OnChanged callback not called within 5 seconds")
	}
}
