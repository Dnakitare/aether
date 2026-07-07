package scheduler_test

import (
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/dnakitare/aether/internal/scheduler"
	"github.com/dnakitare/aether/pkg/api"
)

// TestQueueRemoveAfterSiftingEvictsCorrectAgent guards the index-corruption bug
// where Enqueue set index = len-1 without accounting for heap.Push sifting the
// new item upward. A stale index made Remove evict a different agent.
func TestQueueRemoveAfterSiftingEvictsCorrectAgent(t *testing.T) {
	q := scheduler.NewQueue()

	low := &scheduler.AgentRequest{
		Config:    api.AgentConfig{ID: api.AgentID("low"), TenantID: api.TenantID("t1")},
		Priority:  1,
		CreatedAt: time.Now(),
	}
	high := &scheduler.AgentRequest{
		Config:    api.AgentConfig{ID: api.AgentID("high"), TenantID: api.TenantID("t1")},
		Priority:  10, // sifts above "low", so heap positions differ from insertion order
		CreatedAt: time.Now(),
	}

	q.Enqueue(low)
	q.Enqueue(high)

	// Removing "low" must remove exactly "low" and leave "high" queued.
	if !q.Remove(api.AgentID("low")) {
		t.Fatalf("Remove(low) = false, want true")
	}
	if q.Len() != 1 {
		t.Fatalf("queue length after remove = %d, want 1", q.Len())
	}
	remaining := q.Peek()
	if remaining == nil || remaining.Config.ID != api.AgentID("high") {
		t.Fatalf("remaining agent = %v, want \"high\" (Remove evicted the wrong agent)", remaining)
	}
}

// TestNodeSnapshotConcurrentWithAllocation guards the crash vector where the API
// serialized live *Node pointers while the scheduler mutated their maps, a Go
// fatal ("concurrent map iteration and map write"). Snapshot() must be race-free.
func TestNodeSnapshotConcurrentWithAllocation(t *testing.T) {
	node := &scheduler.Node{
		ID:       "node-1",
		Name:     "node-1",
		Labels:   map[string]string{"zone": "a"},
		Capacity: scheduler.Resources{CPUCores: 100000, MemoryMB: 100000, DiskMB: 100000},
		Agents:   map[api.AgentID]*scheduler.AgentAllocation{},
	}

	var wg sync.WaitGroup
	stop := make(chan struct{})

	// Writer: continuously allocate and deallocate agents.
	wg.Add(1)
	go func() {
		defer wg.Done()
		i := 0
		for {
			select {
			case <-stop:
				return
			default:
				id := api.AgentID(fmt.Sprintf("agent-%d", i))
				node.Allocate(id, api.TenantID("t1"), scheduler.Resources{CPUCores: 1, MemoryMB: 1, DiskMB: 1})
				node.Deallocate(id)
				i++
			}
		}
	}()

	// Reader: snapshot and marshal, which is exactly what the API handler does.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 2000; i++ {
			snap := node.Snapshot()
			if _, err := json.Marshal(snap); err != nil {
				t.Errorf("marshal snapshot: %v", err)
				return
			}
		}
	}()

	// Let the reader finish its iterations, then stop the writer.
	go func() {
		time.Sleep(50 * time.Millisecond)
		close(stop)
	}()

	wg.Wait()
}
