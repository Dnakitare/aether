// Package scheduler provides agent scheduling and placement.
package scheduler

import (
	"time"

	"github.com/dnakitare/aether/pkg/api"
)

// Node represents a compute node that can host agents.
// In Phase 2, this is a single node. In Phase 6, this will support multi-node.
type Node struct {
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
	return Resources{
		CPUCores: n.Capacity.CPUCores - n.Allocated.CPUCores,
		MemoryMB: n.Capacity.MemoryMB - n.Allocated.MemoryMB,
		DiskMB:   n.Capacity.DiskMB - n.Allocated.DiskMB,
	}
}

// CanFit checks if the node can accommodate the requested resources.
func (n *Node) CanFit(req Resources) bool {
	avail := n.Available()
	return avail.CPUCores >= req.CPUCores &&
		avail.MemoryMB >= req.MemoryMB &&
		avail.DiskMB >= req.DiskMB
}

// Allocate reserves resources on this node for an agent.
func (n *Node) Allocate(agentID api.AgentID, tenantID api.TenantID, resources Resources) {
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
	if n.Capacity.CPUCores == 0 {
		return 0
	}
	return float64(n.Allocated.CPUCores) / float64(n.Capacity.CPUCores) * 100
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
