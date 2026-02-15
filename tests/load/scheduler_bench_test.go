package load

import (
	"fmt"
	"testing"

	"github.com/aether-runtime/aether/internal/scheduler"
	"github.com/aether-runtime/aether/pkg/api"
)

// BenchmarkSchedulerPlacement benchmarks the core scheduling algorithm
func BenchmarkSchedulerPlacement(b *testing.B) {
	// Create a simple in-memory scheduler for benchmarking
	nodes := createBenchmarkNodes(10)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		agentReq := CreateTestAgentRequest(i, "tenant-1")

		// Simulate bin-packing placement
		for _, node := range nodes {
			if node.CanFit(agentReq.Resources) {
				// Found a suitable node
				break
			}
		}
	}
}

// BenchmarkSchedulerPlacement_BinPacking benchmarks bin-packing strategy
func BenchmarkSchedulerPlacement_BinPacking(b *testing.B) {
	nodes := createBenchmarkNodes(100)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		agentReq := CreateTestAgentRequest(i, "tenant-1")

		// Bin-packing: find node with most allocated resources that still fits
		var bestNode *scheduler.Node
		var maxUtilization float64

		for _, node := range nodes {
			if node.CanFit(agentReq.Resources) {
				util := float64(node.Allocated.CPUCores) / float64(node.Capacity.CPUCores)
				if util > maxUtilization {
					maxUtilization = util
					bestNode = node
				}
			}
		}

		if bestNode != nil {
			// Would place here
			_ = bestNode
		}
	}
}

// BenchmarkSchedulerPlacement_Spread benchmarks spread strategy
func BenchmarkSchedulerPlacement_Spread(b *testing.B) {
	nodes := createBenchmarkNodes(100)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		agentReq := CreateTestAgentRequest(i, "tenant-1")

		// Spread: find node with least allocated resources
		var bestNode *scheduler.Node
		minUtilization := 1.0

		for _, node := range nodes {
			if node.CanFit(agentReq.Resources) {
				util := float64(node.Allocated.CPUCores) / float64(node.Capacity.CPUCores)
				if util < minUtilization {
					minUtilization = util
					bestNode = node
				}
			}
		}

		if bestNode != nil {
			// Would place here
			_ = bestNode
		}
	}
}

// BenchmarkNodeCanFit benchmarks resource fit checking
func BenchmarkNodeCanFit(b *testing.B) {
	node := &scheduler.Node{
		ID:   "bench-node-1",
		Name: "Benchmark Node",
		Capacity: scheduler.Resources{
			CPUCores: 100000,
			MemoryMB: 204800,
			DiskMB:   2097152,
		},
		Allocated: scheduler.Resources{
			CPUCores: 50000,
			MemoryMB: 102400,
			DiskMB:   1048576,
		},
		Agents: make(map[api.AgentID]*scheduler.AgentAllocation),
	}

	resources := scheduler.Resources{
		CPUCores: 1000,
		MemoryMB: 2048,
		DiskMB:   10240,
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = node.CanFit(resources)
	}
}

// BenchmarkAgentRequestCreation benchmarks creating agent requests
func BenchmarkAgentRequestCreation(b *testing.B) {
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = CreateTestAgentRequest(i, "tenant-1")
	}
}

// BenchmarkConcurrentPlacement benchmarks concurrent scheduling operations
func BenchmarkConcurrentPlacement(b *testing.B) {
	nodes := createBenchmarkNodes(50)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			agentReq := CreateTestAgentRequest(i, "tenant-1")

			for _, node := range nodes {
				if node.CanFit(agentReq.Resources) {
					break
				}
			}

			i++
		}
	})
}

// BenchmarkResourceCalculation benchmarks resource utilization calculations
func BenchmarkResourceCalculation(b *testing.B) {
	node := &scheduler.Node{
		ID:   "bench-node-1",
		Name: "Benchmark Node",
		Capacity: scheduler.Resources{
			CPUCores: 100000,
			MemoryMB: 204800,
			DiskMB:   2097152,
		},
		Allocated: scheduler.Resources{},
		Agents:    make(map[api.AgentID]*scheduler.AgentAllocation),
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		// Calculate utilization
		cpuUtil := float64(node.Allocated.CPUCores) / float64(node.Capacity.CPUCores)
		memUtil := float64(node.Allocated.MemoryMB) / float64(node.Capacity.MemoryMB)
		diskUtil := float64(node.Allocated.DiskMB) / float64(node.Capacity.DiskMB)

		_ = cpuUtil + memUtil + diskUtil
	}
}

// BenchmarkScheduler_1000Nodes benchmarks scheduling with 1000 nodes
func BenchmarkScheduler_1000Nodes(b *testing.B) {
	nodes := createBenchmarkNodes(1000)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		agentReq := CreateTestAgentRequest(i, "tenant-1")

		for _, node := range nodes {
			if node.CanFit(agentReq.Resources) {
				break
			}
		}
	}
}

// BenchmarkScheduler_WithTenantIsolation benchmarks scheduling with tenant isolation
func BenchmarkScheduler_WithTenantIsolation(b *testing.B) {
	nodes := createBenchmarkNodes(100)

	// Pre-allocate some nodes to specific tenants
	for i, node := range nodes {
		tenantID := fmt.Sprintf("tenant-%d", i%5)
		node.Labels["tenant"] = tenantID
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		tenantID := fmt.Sprintf("tenant-%d", i%5)
		agentReq := CreateTestAgentRequest(i, tenantID)

		// Find node for specific tenant
		for _, node := range nodes {
			if node.Labels["tenant"] == tenantID && node.CanFit(agentReq.Resources) {
				break
			}
		}
	}
}

// Helper function to create benchmark nodes
func createBenchmarkNodes(count int) []*scheduler.Node {
	nodes := make([]*scheduler.Node, count)

	for i := 0; i < count; i++ {
		nodes[i] = &scheduler.Node{
			ID:   fmt.Sprintf("bench-node-%04d", i),
			Name: fmt.Sprintf("Benchmark Node %d", i),
			Labels: map[string]string{
				"region": "us-west-2",
				"zone":   fmt.Sprintf("us-west-2%c", 'a'+(i%3)),
			},
			Capacity: scheduler.Resources{
				CPUCores: 200000,
				MemoryMB: 409600,
				DiskMB:   4194304,
			},
			Allocated: scheduler.Resources{
				CPUCores: int64(i * 1000), // Vary allocated resources
				MemoryMB: int64(i * 2048),
				DiskMB:   int64(i * 10240),
			},
			Agents: make(map[api.AgentID]*scheduler.AgentAllocation),
		}
	}

	return nodes
}

// BenchmarkSchedulerStats benchmarks collecting scheduler statistics
func BenchmarkSchedulerStats(b *testing.B) {
	nodes := createBenchmarkNodes(100)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		// Simulate stats collection
		var totalCPU, totalMem, totalDisk int64
		var allocCPU, allocMem, allocDisk int64

		for _, node := range nodes {
			totalCPU += node.Capacity.CPUCores
			totalMem += node.Capacity.MemoryMB
			totalDisk += node.Capacity.DiskMB

			allocCPU += node.Allocated.CPUCores
			allocMem += node.Allocated.MemoryMB
			allocDisk += node.Allocated.DiskMB
		}

		_ = totalCPU + totalMem + totalDisk + allocCPU + allocMem + allocDisk
	}
}

// Example output format for benchmark results:
/*
BenchmarkSchedulerPlacement-8                  	 1000000	      1234 ns/op
BenchmarkSchedulerPlacement_BinPacking-8       	  500000	      2345 ns/op
BenchmarkSchedulerPlacement_Spread-8           	  500000	      2456 ns/op
BenchmarkNodeCanFit-8                          	100000000	        12.3 ns/op
BenchmarkConcurrentPlacement-8                 	 5000000	       345 ns/op
BenchmarkScheduler_1000Nodes-8                 	  100000	     12345 ns/op
*/
