package vm_test

import (
	"testing"

	"github.com/aether-runtime/aether/internal/runtime/vm"
	"github.com/aether-runtime/aether/pkg/api"
)

func TestFromAgentConfig(t *testing.T) {
	agentConfig := api.AgentConfig{
		ID:       api.AgentID("test-agent-123"),
		TenantID: api.TenantID("tenant-1"),
		Name:     "Test Agent",
		Image:    "python:3.11",
		Resources: api.ResourceLimits{
			CPUCount: 2,
			MemoryMB: 1024,
		},
	}

	paths := vm.VMPaths{
		KernelImage: "/path/to/kernel",
		RootFS:      "/path/to/rootfs",
		Socket:      "/path/to/socket",
		Log:         "/path/to/log",
		Metrics:     "/path/to/metrics",
		WorkDir:     "/path/to/workdir",
	}

	vmConfig := vm.FromAgentConfig(agentConfig, paths)

	if vmConfig.ID != string(agentConfig.ID) {
		t.Errorf("ID = %s, want %s", vmConfig.ID, agentConfig.ID)
	}

	if vmConfig.CPUCount != agentConfig.Resources.CPUCount {
		t.Errorf("CPUCount = %d, want %d", vmConfig.CPUCount, agentConfig.Resources.CPUCount)
	}

	if vmConfig.MemoryMB != agentConfig.Resources.MemoryMB {
		t.Errorf("MemoryMB = %d, want %d", vmConfig.MemoryMB, agentConfig.Resources.MemoryMB)
	}

	if vmConfig.KernelImagePath != paths.KernelImage {
		t.Errorf("KernelImagePath = %s, want %s", vmConfig.KernelImagePath, paths.KernelImage)
	}

	if len(vmConfig.NetworkInterfaces) != 1 {
		t.Errorf("NetworkInterfaces length = %d, want 1", len(vmConfig.NetworkInterfaces))
	}
}

func TestVMConfig(t *testing.T) {
	config := vm.VMConfig{
		ID:              "vm-1",
		KernelImagePath: "/kernel",
		RootfsPath:      "/rootfs",
		BootArgs:        "console=ttyS0",
		CPUCount:        1,
		MemoryMB:        512,
		SocketPath:      "/socket",
		LogPath:         "/log",
		MetricsPath:     "/metrics",
	}

	if config.ID != "vm-1" {
		t.Errorf("ID = %s, want vm-1", config.ID)
	}

	if config.CPUCount != 1 {
		t.Errorf("CPUCount = %d, want 1", config.CPUCount)
	}

	if config.MemoryMB != 512 {
		t.Errorf("MemoryMB = %d, want 512", config.MemoryMB)
	}
}
