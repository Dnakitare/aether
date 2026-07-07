// Package vm provides Firecracker microVM management.
package vm

import (
	"github.com/dnakitare/aether/pkg/api"
)

// VMConfig defines the configuration for a Firecracker microVM.
type VMConfig struct {
	// ID is the unique identifier for this VM.
	ID string

	// KernelImagePath is the path to the Linux kernel image.
	KernelImagePath string

	// RootfsPath is the path to the root filesystem image.
	RootfsPath string

	// BootArgs are kernel boot arguments.
	BootArgs string

	// CPUCount is the number of vCPUs.
	CPUCount int

	// MemoryMB is the amount of memory in megabytes.
	MemoryMB int64

	// NetworkInterfaces defines network configuration.
	NetworkInterfaces []NetworkInterface

	// SocketPath is the path to the Firecracker API socket.
	SocketPath string

	// LogPath is the path to the VM logs.
	LogPath string

	// MetricsPath is the path to the metrics FIFO.
	MetricsPath string
}

// NetworkInterface defines a network interface for the VM.
type NetworkInterface struct {
	// ID is the interface identifier.
	ID string

	// HostDevName is the tap device name on the host.
	HostDevName string

	// GuestMAC is the MAC address inside the guest.
	GuestMAC string

	// AllowMMDS enables the Metadata Service.
	AllowMMDS bool
}

// FromAgentConfig creates a VMConfig from an AgentConfig.
func FromAgentConfig(agentConfig api.AgentConfig, paths VMPaths) VMConfig {
	return VMConfig{
		ID:              string(agentConfig.ID),
		KernelImagePath: paths.KernelImage,
		RootfsPath:      paths.RootFS,
		BootArgs:        "console=ttyS0 reboot=k panic=1 pci=off",
		CPUCount:        agentConfig.Resources.CPUCount,
		MemoryMB:        agentConfig.Resources.MemoryMB,
		NetworkInterfaces: []NetworkInterface{
			{
				ID:          "eth0",
				HostDevName: tapDevName(string(agentConfig.ID)),
				GuestMAC:    generateMAC(string(agentConfig.ID)),
				AllowMMDS:   true,
			},
		},
		SocketPath:  paths.Socket,
		LogPath:     paths.Log,
		MetricsPath: paths.Metrics,
	}
}

// VMPaths contains file paths for VM resources.
type VMPaths struct {
	// KernelImage is the path to the kernel.
	KernelImage string

	// RootFS is the path to the root filesystem.
	RootFS string

	// Socket is the API socket path.
	Socket string

	// Log is the log file path.
	Log string

	// Metrics is the metrics FIFO path.
	Metrics string

	// WorkDir is the working directory for this VM.
	WorkDir string
}

// tapDevName derives the host TAP interface name for an agent. It uses the
// first 8 characters of the agent ID, padding short IDs so the slice never
// panics. The result stays within Linux's 15-char interface-name limit.
func tapDevName(id string) string {
	if len(id) < 8 {
		id = id + "00000000"
	}
	return "tap" + id[:8]
}

// generateMAC generates a MAC address from an agent ID.
// Uses the locally administered address range.
func generateMAC(id string) string {
	// Simple deterministic MAC generation
	// In production, ensure uniqueness and proper addressing
	if len(id) < 12 {
		id = id + "000000000000"
	}
	return "02:00:" + id[0:2] + ":" + id[2:4] + ":" + id[4:6] + ":" + id[6:8]
}
