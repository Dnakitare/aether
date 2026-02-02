// Package scheduler provides agent scheduling and placement.
package scheduler

import (
	"fmt"
	"sort"

	"github.com/aether-runtime/aether/pkg/api"
)

// PlacementStrategy determines how agents are placed on nodes.
type PlacementStrategy string

const (
	// BinPacking places agents to maximize resource utilization.
	BinPacking PlacementStrategy = "bin-packing"

	// Spread distributes agents evenly across nodes.
	Spread PlacementStrategy = "spread"

	// BestFit places agents on nodes with best resource fit.
	BestFit PlacementStrategy = "best-fit"
)

// Placer handles agent placement decisions.
type Placer struct {
	strategy PlacementStrategy
}

// NewPlacer creates a new placement engine.
func NewPlacer(strategy PlacementStrategy) *Placer {
	return &Placer{
		strategy: strategy,
	}
}

// SelectNode selects the best node for an agent request.
func (p *Placer) SelectNode(req *AgentRequest, nodes []*Node) (*Node, error) {
	// Filter nodes that can fit the request
	candidates := p.filterCandidates(req, nodes)
	if len(candidates) == 0 {
		return nil, fmt.Errorf("no nodes available with sufficient resources")
	}

	// Apply placement strategy
	switch p.strategy {
	case BinPacking:
		return p.binPacking(req, candidates), nil
	case Spread:
		return p.spread(req, candidates), nil
	case BestFit:
		return p.bestFit(req, candidates), nil
	default:
		return p.binPacking(req, candidates), nil
	}
}

// filterCandidates returns nodes that can accommodate the request.
func (p *Placer) filterCandidates(req *AgentRequest, nodes []*Node) []*Node {
	candidates := make([]*Node, 0)

	for _, node := range nodes {
		// Check resource fit
		if !node.CanFit(req.Resources) {
			continue
		}

		// Check node selector constraints
		if len(req.Constraints.NodeSelector) > 0 {
			if !matchesLabels(node.Labels, req.Constraints.NodeSelector) {
				continue
			}
		}

		// Check exclusive node constraint
		if req.Constraints.RequireExclusiveNode && len(node.Agents) > 0 {
			continue
		}

		// Check anti-affinity constraint
		if req.Constraints.AntiAffinityTenant {
			if hasAgentFromTenant(node, req.Config.TenantID) {
				continue
			}
		}

		candidates = append(candidates, node)
	}

	return candidates
}

// binPacking selects the node with highest utilization that can fit the request.
// This maximizes resource utilization and minimizes fragmentation.
func (p *Placer) binPacking(req *AgentRequest, nodes []*Node) *Node {
	sort.Slice(nodes, func(i, j int) bool {
		// Sort by utilization (descending)
		return nodes[i].UtilizationPercent() > nodes[j].UtilizationPercent()
	})

	return nodes[0]
}

// spread selects the node with lowest utilization.
// This distributes agents evenly across nodes.
func (p *Placer) spread(req *AgentRequest, nodes []*Node) *Node {
	sort.Slice(nodes, func(i, j int) bool {
		// Sort by utilization (ascending)
		return nodes[i].UtilizationPercent() < nodes[j].UtilizationPercent()
	})

	return nodes[0]
}

// bestFit selects the node with least wasted resources after placement.
func (p *Placer) bestFit(req *AgentRequest, nodes []*Node) *Node {
	type nodeScore struct {
		node  *Node
		waste int64
	}

	scores := make([]nodeScore, 0, len(nodes))

	for _, node := range nodes {
		avail := node.Available()
		// Calculate wasted CPU (in millicores)
		cpuWaste := avail.CPUCores - req.Resources.CPUCores
		// Calculate wasted memory (in MB)
		memWaste := avail.MemoryMB - req.Resources.MemoryMB

		// Combined waste score (normalize to same scale)
		waste := cpuWaste/10 + memWaste

		scores = append(scores, nodeScore{node: node, waste: waste})
	}

	// Sort by waste (ascending - less waste is better)
	sort.Slice(scores, func(i, j int) bool {
		return scores[i].waste < scores[j].waste
	})

	return scores[0].node
}

// matchesLabels checks if node labels match the selector.
func matchesLabels(nodeLabels, selector map[string]string) bool {
	for key, value := range selector {
		if nodeLabels[key] != value {
			return false
		}
	}
	return true
}

// hasAgentFromTenant checks if a node has any agents from the given tenant.
func hasAgentFromTenant(node *Node, tenantID api.TenantID) bool {
	for _, alloc := range node.Agents {
		if alloc.TenantID == tenantID {
			return true
		}
	}
	return false
}
