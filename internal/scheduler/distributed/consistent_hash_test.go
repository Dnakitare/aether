package distributed

import (
	"fmt"
	"testing"
)

func TestConsistentHash_AddRemove(t *testing.T) {
	ch := NewConsistentHash(10)

	// Add schedulers
	ch.Add("scheduler-1")
	ch.Add("scheduler-2")
	ch.Add("scheduler-3")

	if count := ch.Count(); count != 3 {
		t.Errorf("Count() = %d, want 3", count)
	}

	members := ch.Members()
	if len(members) != 3 {
		t.Errorf("Members() length = %d, want 3", len(members))
	}

	// Remove scheduler
	ch.Remove("scheduler-2")

	if count := ch.Count(); count != 2 {
		t.Errorf("Count() after remove = %d, want 2", count)
	}

	members = ch.Members()
	if len(members) != 2 {
		t.Errorf("Members() length after remove = %d, want 2", len(members))
	}

	// Verify scheduler-2 is gone
	for _, member := range members {
		if member == "scheduler-2" {
			t.Error("scheduler-2 still in members after remove")
		}
	}
}

func TestConsistentHash_Get(t *testing.T) {
	ch := NewConsistentHash(10)

	// Empty ring
	if scheduler := ch.Get("node-001"); scheduler != "" {
		t.Errorf("Get() on empty ring = %q, want empty string", scheduler)
	}

	// Add schedulers
	ch.Add("scheduler-1")
	ch.Add("scheduler-2")
	ch.Add("scheduler-3")

	// Same key should always map to same scheduler
	scheduler1 := ch.Get("node-001")
	scheduler2 := ch.Get("node-001")

	if scheduler1 != scheduler2 {
		t.Errorf("Get() not consistent: %q != %q", scheduler1, scheduler2)
	}

	if scheduler1 == "" {
		t.Error("Get() returned empty string for non-empty ring")
	}

	// Different keys should map (possibly different schedulers)
	scheduler3 := ch.Get("node-002")
	if scheduler3 == "" {
		t.Error("Get() returned empty string for node-002")
	}
}

func TestConsistentHash_GetMultiple(t *testing.T) {
	ch := NewConsistentHash(10)

	ch.Add("scheduler-1")
	ch.Add("scheduler-2")
	ch.Add("scheduler-3")

	// Get 2 schedulers for replication
	schedulers := ch.GetMultiple("node-001", 2)

	if len(schedulers) != 2 {
		t.Errorf("GetMultiple(2) returned %d schedulers, want 2", len(schedulers))
	}

	// Verify unique schedulers
	if len(schedulers) == 2 && schedulers[0] == schedulers[1] {
		t.Error("GetMultiple() returned duplicate schedulers")
	}

	// Get all schedulers
	all := ch.GetMultiple("node-001", 10)
	if len(all) != 3 {
		t.Errorf("GetMultiple(10) returned %d schedulers, want 3", len(all))
	}
}

func TestConsistentHash_Distribution(t *testing.T) {
	// Use 1000 virtual nodes for better distribution
	ch := NewConsistentHash(1000)

	// Add 3 schedulers
	ch.Add("scheduler-1")
	ch.Add("scheduler-2")
	ch.Add("scheduler-3")

	// Generate 10000 keys for statistical significance
	keys := make([]string, 10000)
	for i := 0; i < 10000; i++ {
		keys[i] = fmt.Sprintf("node-%05d", i)
	}

	dist := ch.Distribution(keys)

	// Each scheduler should get approximately 10000/3 = 3333 keys
	// Allow 25% variance (2500-4166 keys) - consistent hashing doesn't guarantee perfect distribution
	// The key property is stability on node changes, not perfect uniformity
	for scheduler, count := range dist {
		if count < 2500 || count > 4166 {
			t.Errorf("Scheduler %s has %d keys, expected ~3333 (2500-4166)", scheduler, count)
		}
		t.Logf("Scheduler %s: %d keys (%.1f%%)", scheduler, count, float64(count)/100)
	}

	// Verify all keys assigned
	total := 0
	for _, count := range dist {
		total += count
	}
	if total != 10000 {
		t.Errorf("Total keys assigned = %d, want 10000", total)
	}
}

func TestConsistentHash_StabilityOnRemove(t *testing.T) {
	ch := NewConsistentHash(100)

	ch.Add("scheduler-1")
	ch.Add("scheduler-2")
	ch.Add("scheduler-3")

	// Map 100 keys before removal
	keyAssignments := make(map[string]string)
	for i := 0; i < 100; i++ {
		key := fmt.Sprintf("node-%03d", i)
		keyAssignments[key] = ch.Get(key)
	}

	// Remove scheduler-2
	ch.Remove("scheduler-2")

	// Check how many keys moved
	moved := 0
	for key, oldScheduler := range keyAssignments {
		newScheduler := ch.Get(key)

		if oldScheduler == "scheduler-2" {
			// Keys from removed scheduler must move
			if newScheduler == "scheduler-2" {
				t.Errorf("Key %s still assigned to removed scheduler-2", key)
			}
		} else if oldScheduler != newScheduler {
			// Keys from other schedulers should mostly stay
			moved++
		}
	}

	// With consistent hashing, only keys from scheduler-2 should move
	// (plus a small number due to hash ring rebalancing)
	// In practice, about 1/3 of keys were on scheduler-2, so ~33 keys should move
	// Allow up to 50 keys moved (includes scheduler-2 keys + some rebalancing)
	if moved > 50 {
		t.Errorf("Too many keys moved on removal: %d (expected < 50)", moved)
	}
}

func TestConsistentHash_Concurrent(t *testing.T) {
	ch := NewConsistentHash(100)

	ch.Add("scheduler-1")
	ch.Add("scheduler-2")

	// Concurrent reads
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func(n int) {
			for j := 0; j < 100; j++ {
				key := fmt.Sprintf("node-%d-%d", n, j)
				_ = ch.Get(key)
			}
			done <- true
		}(i)
	}

	// Concurrent writes
	go func() {
		ch.Add("scheduler-3")
		ch.Remove("scheduler-1")
		ch.Add("scheduler-4")
		done <- true
	}()

	// Wait for all goroutines
	for i := 0; i < 11; i++ {
		<-done
	}

	// If we get here without panic, concurrent access is safe
}

func TestConsistentHash_ZeroReplicas(t *testing.T) {
	// Should use default replicas
	ch := NewConsistentHash(0)

	ch.Add("scheduler-1")
	scheduler := ch.Get("node-001")

	if scheduler != "scheduler-1" {
		t.Errorf("Get() = %q, want scheduler-1", scheduler)
	}
}

func BenchmarkConsistentHash_Get(b *testing.B) {
	ch := NewConsistentHash(100)

	for i := 0; i < 10; i++ {
		ch.Add(fmt.Sprintf("scheduler-%d", i))
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		key := fmt.Sprintf("node-%d", i%1000)
		_ = ch.Get(key)
	}
}

func BenchmarkConsistentHash_Add(b *testing.B) {
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		ch := NewConsistentHash(100)
		ch.Add(fmt.Sprintf("scheduler-%d", i))
	}
}

func BenchmarkConsistentHash_Remove(b *testing.B) {
	ch := NewConsistentHash(100)

	// Pre-add schedulers
	for i := 0; i < 1000; i++ {
		ch.Add(fmt.Sprintf("scheduler-%d", i))
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		ch.Remove(fmt.Sprintf("scheduler-%d", i%1000))
	}
}
