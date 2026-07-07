package vm_test

import (
	"testing"

	"github.com/dnakitare/aether/internal/runtime/vm"
	"github.com/dnakitare/aether/pkg/api"
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

// TestFromAgentConfigShortIDNoPanic guards a slice-out-of-range panic: agent IDs
// as short as one character pass ValidateAgentID, but the TAP device name used
// to derive the first 8 chars of the ID. Short IDs must be padded, not sliced raw.
func TestFromAgentConfigShortIDNoPanic(t *testing.T) {
	paths := vm.VMPaths{KernelImage: "/k", RootFS: "/r", Socket: "/s", Log: "/l", Metrics: "/m", WorkDir: "/w"}

	for _, id := range []string{"a", "ab", "abc", "1234567", "12345678"} {
		agentConfig := api.AgentConfig{
			ID:        api.AgentID(id),
			TenantID:  api.TenantID("tenant-1"),
			Resources: api.ResourceLimits{CPUCount: 1, MemoryMB: 256},
		}

		vmConfig := vm.FromAgentConfig(agentConfig, paths) // must not panic
		if len(vmConfig.NetworkInterfaces) != 1 {
			t.Fatalf("id %q: NetworkInterfaces length = %d, want 1", id, len(vmConfig.NetworkInterfaces))
		}
		dev := vmConfig.NetworkInterfaces[0].HostDevName
		if len(dev) != len("tap")+8 {
			t.Errorf("id %q: HostDevName = %q, want length %d", id, dev, len("tap")+8)
		}
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
