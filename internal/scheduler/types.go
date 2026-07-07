// Package scheduler provides agent scheduling and placement.
package scheduler

import (
	"sync"
	"time"

	"github.com/dnakitare/aether/pkg/api"
)

// Node represents a compute node that can host agents.
// In Phase 2, this is a single node. In Phase 6, this will support multi-node.
type Node struct {
	mu        sync.RWMutex // Protects Allocated and Agents
	ID        string
	Name      string
	Labels    map[string]string
	Capacity  Resources
	Allocated Resources
	Agents    map[api.AgentID]*AgentAllocation
}

// Resources represents compute resources.
type Resources struct {
	CPUCores int64 // CPU cores in millicores (1000 = 1 core)
	MemoryMB int64 // Memory in megabytes
	DiskMB   int64 // Disk in megabytes
}

// AgentAllocation tracks an agent's resource allocation on a node.
type AgentAllocation struct {
	AgentID   api.AgentID
	TenantID  api.TenantID
	Resources Resources
	NodeID    string
	Status    api.AgentStatus
	CreatedAt time.Time
}

// Available returns the available resources on this node.
func (n *Node) Available() Resources {
	n.mu.RLock()
	defer n.mu.RUnlock()

	return Resources{
		CPUCores: n.Capacity.CPUCores - n.Allocated.CPUCores,
		MemoryMB: n.Capacity.MemoryMB - n.Allocated.MemoryMB,
		DiskMB:   n.Capacity.DiskMB - n.Allocated.DiskMB,
	}
}

// CanFit checks if the node can accommodate the requested resources.
func (n *Node) CanFit(req Resources) bool {
	n.mu.RLock()
	defer n.mu.RUnlock()

	avail := Resources{
		CPUCores: n.Capacity.CPUCores - n.Allocated.CPUCores,
		MemoryMB: n.Capacity.MemoryMB - n.Allocated.MemoryMB,
		DiskMB:   n.Capacity.DiskMB - n.Allocated.DiskMB,
	}
	return avail.CPUCores >= req.CPUCores &&
		avail.MemoryMB >= req.MemoryMB &&
		avail.DiskMB >= req.DiskMB
}

// Allocate reserves resources on this node for an agent.
func (n *Node) Allocate(agentID api.AgentID, tenantID api.TenantID, resources Resources) {
	n.mu.Lock()
	defer n.mu.Unlock()

	n.Allocated.CPUCores += resources.CPUCores
	n.Allocated.MemoryMB += resources.MemoryMB
	n.Allocated.DiskMB += resources.DiskMB

	n.Agents[agentID] = &AgentAllocation{
		AgentID:   agentID,
		TenantID:  tenantID,
		Resources: resources,
		NodeID:    n.ID,
		Status:    api.AgentStatusPending,
		CreatedAt: time.Now(),
	}
}

// Deallocate releases resources on this node.
func (n *Node) Deallocate(agentID api.AgentID) {
	n.mu.Lock()
	defer n.mu.Unlock()

	alloc, exists := n.Agents[agentID]
	if !exists {
		return
	}

	n.Allocated.CPUCores -= alloc.Resources.CPUCores
	n.Allocated.MemoryMB -= alloc.Resources.MemoryMB
	n.Allocated.DiskMB -= alloc.Resources.DiskMB

	delete(n.Agents, agentID)
}

// UtilizationPercent returns CPU utilization as a percentage (0-100).
func (n *Node) UtilizationPercent() float64 {
	n.mu.RLock()
	defer n.mu.RUnlock()

	if n.Capacity.CPUCores == 0 {
		return 0
	}
	return float64(n.Allocated.CPUCores) / float64(n.Capacity.CPUCores) * 100
}

// AgentCount returns the number of agents on this node.
func (n *Node) AgentCount() int {
	n.mu.RLock()
	defer n.mu.RUnlock()

	return len(n.Agents)
}

// HasAgent returns true if the node has the specified agent.
func (n *Node) HasAgent(agentID api.AgentID) bool {
	n.mu.RLock()
	defer n.mu.RUnlock()

	_, exists := n.Agents[agentID]
	return exists
}

// AgentIDs returns a slice of agent IDs on this node.
func (n *Node) AgentIDs() []string {
	n.mu.RLock()
	defer n.mu.RUnlock()

	ids := make([]string, 0, len(n.Agents))
	for agentID := range n.Agents {
		ids = append(ids, string(agentID))
	}
	return ids
}

// RLock acquires a read lock on the node (for external serialization).
func (n *Node) RLock() {
	n.mu.RLock()
}

// RUnlock releases a read lock on the node (for external serialization).
func (n *Node) RUnlock() {
	n.mu.RUnlock()
}

// NodeSnapshot is an immutable, lock-free copy of a Node's state, safe to
// hand to callers (e.g. JSON serialization) without racing the scheduler.
type NodeSnapshot struct {
	ID        string                           `json:"id"`
	Name      string                           `json:"name"`
	Labels    map[string]string                `json:"labels"`
	Capacity  Resources                        `json:"capacity"`
	Allocated Resources                        `json:"allocated"`
	Available Resources                        `json:"available"`
	Agents    map[api.AgentID]*AgentAllocation `json:"agents"`
}

// Snapshot returns a deep copy of the node's state taken under the node's read
// lock. Callers must use this rather than the live *Node when serializing,
// because marshaling the live node reads its maps while the scheduler mutates
// them (a Go fatal: "concurrent map iteration and map write").
func (n *Node) Snapshot() NodeSnapshot {
	n.mu.RLock()
	defer n.mu.RUnlock()

	labels := make(map[string]string, len(n.Labels))
	for k, v := range n.Labels {
		labels[k] = v
	}

	agents := make(map[api.AgentID]*AgentAllocation, len(n.Agents))
	for id, alloc := range n.Agents {
		allocCopy := *alloc
		agents[id] = &allocCopy
	}

	return NodeSnapshot{
		ID:        n.ID,
		Name:      n.Name,
		Labels:    labels,
		Capacity:  n.Capacity,
		Allocated: n.Allocated,
		Available: Resources{
			CPUCores: n.Capacity.CPUCores - n.Allocated.CPUCores,
			MemoryMB: n.Capacity.MemoryMB - n.Allocated.MemoryMB,
			DiskMB:   n.Capacity.DiskMB - n.Allocated.DiskMB,
		},
		Agents: agents,
	}
}

// AgentRequest represents a request to schedule an agent.
type AgentRequest struct {
	Config      api.AgentConfig
	Resources   Resources
	Constraints SchedulingConstraints
	Priority    int // Higher priority = scheduled first
	CreatedAt   time.Time
}

// SchedulingConstraints defines placement requirements.
type SchedulingConstraints struct {
	// NodeSelector requires specific node labels.
	NodeSelector map[string]string

	// AntiAffinity prevents co-location with agents from same tenant.
	AntiAffinityTenant bool

	// RequireExclusiveNode prevents sharing the node with other agents.
	RequireExclusiveNode bool
}

// FromAgentConfig creates a Resources struct from AgentConfig.
func FromAgentConfig(config api.AgentConfig) Resources {
	cpuMillicores := int64(config.Resources.CPUCount) * 1000
	memoryMB := config.Resources.MemoryMB
	diskMB := config.Resources.DiskMB

	if diskMB == 0 {
		diskMB = 10240 // Default 10GB
	}

	return Resources{
		CPUCores: cpuMillicores,
		MemoryMB: memoryMB,
		DiskMB:   diskMB,
	}
}
