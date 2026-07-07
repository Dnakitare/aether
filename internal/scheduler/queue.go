// Package scheduler provides agent scheduling and placement.
package scheduler

import (
	"container/heap"
	"sync"

	"github.com/dnakitare/aether/pkg/api"
)

// Queue is a priority queue for agent scheduling requests.
type Queue struct {
	mu    sync.RWMutex
	items priorityQueue
	index map[api.AgentID]int // Track items by agent ID for fast lookup
}

// NewQueue creates a new scheduling queue.
func NewQueue() *Queue {
	q := &Queue{
		items: make(priorityQueue, 0),
		index: make(map[api.AgentID]int),
	}
	heap.Init(&q.items)
	return q
}

// Enqueue adds an agent request to the queue.
func (q *Queue) Enqueue(req *AgentRequest) {
	q.mu.Lock()
	defer q.mu.Unlock()

	heap.Push(&q.items, req)
	// heap.Push sifts the new item up via Swap, which does not maintain
	// q.index, so the final position isn't len-1. Rebuild the whole index
	// (as Dequeue/Remove do) so a subsequent Remove can't evict the wrong
	// agent off a stale index.
	q.rebuildIndex()
}

// Dequeue removes and returns the highest priority request.
func (q *Queue) Dequeue() *AgentRequest {
	q.mu.Lock()
	defer q.mu.Unlock()

	if len(q.items) == 0 {
		return nil
	}

	req := heap.Pop(&q.items).(*AgentRequest)
	delete(q.index, req.Config.ID)

	// Rebuild index after pop
	q.rebuildIndex()

	return req
}

// Peek returns the highest priority request without removing it.
func (q *Queue) Peek() *AgentRequest {
	q.mu.RLock()
	defer q.mu.RUnlock()

	if len(q.items) == 0 {
		return nil
	}

	return q.items[0]
}

// Remove removes a specific agent request from the queue.
func (q *Queue) Remove(agentID api.AgentID) bool {
	q.mu.Lock()
	defer q.mu.Unlock()

	idx, exists := q.index[agentID]
	if !exists {
		return false
	}

	heap.Remove(&q.items, idx)
	delete(q.index, agentID)
	q.rebuildIndex()

	return true
}

// Len returns the number of items in the queue.
func (q *Queue) Len() int {
	q.mu.RLock()
	defer q.mu.RUnlock()

	return len(q.items)
}

// rebuildIndex rebuilds the agent ID index (call with lock held).
func (q *Queue) rebuildIndex() {
	q.index = make(map[api.AgentID]int)
	for i, item := range q.items {
		q.index[item.Config.ID] = i
	}
}

// priorityQueue implements heap.Interface for AgentRequest.
type priorityQueue []*AgentRequest

func (pq priorityQueue) Len() int { return len(pq) }

func (pq priorityQueue) Less(i, j int) bool {
	// Higher priority comes first
	if pq[i].Priority != pq[j].Priority {
		return pq[i].Priority > pq[j].Priority
	}
	// If same priority, earlier request comes first (FIFO)
	return pq[i].CreatedAt.Before(pq[j].CreatedAt)
}

func (pq priorityQueue) Swap(i, j int) {
	pq[i], pq[j] = pq[j], pq[i]
}

func (pq *priorityQueue) Push(x interface{}) {
	*pq = append(*pq, x.(*AgentRequest))
}

func (pq *priorityQueue) Pop() interface{} {
	old := *pq
	n := len(old)
	item := old[n-1]
	old[n-1] = nil // Avoid memory leak
	*pq = old[0 : n-1]
	return item
}
