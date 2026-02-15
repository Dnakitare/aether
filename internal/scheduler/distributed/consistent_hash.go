// Package distributed provides distributed scheduler components.
package distributed

import (
	"fmt"
	"hash/fnv"
	"sort"
	"sync"
)

// ConsistentHash implements consistent hashing for node-to-scheduler assignment.
type ConsistentHash struct {
	mu       sync.RWMutex
	ring     []uint32          // Sorted hash values
	nodes    map[uint32]string // Hash -> scheduler instance ID
	replicas int               // Virtual nodes per scheduler
}

// NewConsistentHash creates a new consistent hash ring.
func NewConsistentHash(replicas int) *ConsistentHash {
	if replicas <= 0 {
		replicas = 100 // Default: 100 virtual nodes per scheduler
	}

	return &ConsistentHash{
		ring:     make([]uint32, 0),
		nodes:    make(map[uint32]string),
		replicas: replicas,
	}
}

// Add adds a scheduler to the hash ring.
func (ch *ConsistentHash) Add(schedulerID string) {
	ch.mu.Lock()
	defer ch.mu.Unlock()

	for i := 0; i < ch.replicas; i++ {
		vnode := fmt.Sprintf("%s-v%03d", schedulerID, i)
		hash := ch.hash(vnode)
		ch.ring = append(ch.ring, hash)
		ch.nodes[hash] = schedulerID
	}

	// Sort ring for binary search
	sort.Slice(ch.ring, func(i, j int) bool {
		return ch.ring[i] < ch.ring[j]
	})
}

// Remove removes a scheduler from the hash ring.
func (ch *ConsistentHash) Remove(schedulerID string) {
	ch.mu.Lock()
	defer ch.mu.Unlock()

	// Remove all virtual nodes for this scheduler
	newRing := make([]uint32, 0, len(ch.ring))
	for i := 0; i < ch.replicas; i++ {
		vnode := fmt.Sprintf("%s-v%03d", schedulerID, i)
		hash := ch.hash(vnode)
		delete(ch.nodes, hash)
	}

	// Rebuild ring without removed scheduler's vnodes
	for _, h := range ch.ring {
		if _, exists := ch.nodes[h]; exists {
			newRing = append(newRing, h)
		}
	}

	ch.ring = newRing
}

// Get returns the scheduler responsible for the given key.
func (ch *ConsistentHash) Get(key string) string {
	ch.mu.RLock()
	defer ch.mu.RUnlock()

	if len(ch.ring) == 0 {
		return ""
	}

	hash := ch.hash(key)

	// Binary search for the first ring position >= hash
	idx := sort.Search(len(ch.ring), func(i int) bool {
		return ch.ring[i] >= hash
	})

	// Wrap around if past end
	if idx == len(ch.ring) {
		idx = 0
	}

	return ch.nodes[ch.ring[idx]]
}

// GetMultiple returns N schedulers responsible for the key (for replication).
func (ch *ConsistentHash) GetMultiple(key string, count int) []string {
	ch.mu.RLock()
	defer ch.mu.RUnlock()

	if len(ch.ring) == 0 {
		return nil
	}

	if count <= 0 {
		count = 1
	}

	hash := ch.hash(key)
	idx := sort.Search(len(ch.ring), func(i int) bool {
		return ch.ring[i] >= hash
	})

	if idx == len(ch.ring) {
		idx = 0
	}

	// Collect unique schedulers
	schedulers := make([]string, 0, count)
	seen := make(map[string]bool)

	for i := 0; i < len(ch.ring) && len(schedulers) < count; i++ {
		pos := (idx + i) % len(ch.ring)
		scheduler := ch.nodes[ch.ring[pos]]

		if !seen[scheduler] {
			schedulers = append(schedulers, scheduler)
			seen[scheduler] = true
		}
	}

	return schedulers
}

// Members returns all schedulers in the ring.
func (ch *ConsistentHash) Members() []string {
	ch.mu.RLock()
	defer ch.mu.RUnlock()

	seen := make(map[string]bool)
	members := make([]string, 0)

	for _, scheduler := range ch.nodes {
		if !seen[scheduler] {
			members = append(members, scheduler)
			seen[scheduler] = true
		}
	}

	sort.Strings(members)
	return members
}

// Count returns the number of schedulers in the ring.
func (ch *ConsistentHash) Count() int {
	ch.mu.RLock()
	defer ch.mu.RUnlock()

	seen := make(map[string]bool)
	for _, scheduler := range ch.nodes {
		seen[scheduler] = true
	}

	return len(seen)
}

// hash computes the FNV-1a hash of a string.
func (ch *ConsistentHash) hash(key string) uint32 {
	h := fnv.New32a()
	h.Write([]byte(key))
	return h.Sum32()
}

// Distribution returns the distribution of keys across schedulers (for testing).
func (ch *ConsistentHash) Distribution(keys []string) map[string]int {
	ch.mu.RLock()
	defer ch.mu.RUnlock()

	dist := make(map[string]int)

	for _, key := range keys {
		scheduler := ch.Get(key)
		if scheduler != "" {
			dist[scheduler]++
		}
	}

	return dist
}
