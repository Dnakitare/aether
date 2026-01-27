// Package vm provides Firecracker microVM management.
package vm

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
)

// Manager manages Firecracker VM lifecycle.
type Manager struct {
	logger         *slog.Logger
	firecrackerBin string
	workspaceDir   string
	kernelImage    string
	rootfsImage    string
}

// ManagerConfig configures the VM manager.
type ManagerConfig struct {
	// FirecrackerBinary is the path to the Firecracker binary.
	FirecrackerBinary string

	// WorkspaceDir is where VM files are stored.
	WorkspaceDir string

	// KernelImage is the default kernel image path.
	KernelImage string

	// RootFSImage is the default root filesystem image path.
	RootFSImage string
}

// NewManager creates a new VM manager.
func NewManager(logger *slog.Logger, cfg ManagerConfig) (*Manager, error) {
	// Ensure workspace directory exists
	if err := os.MkdirAll(cfg.WorkspaceDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create workspace directory: %w", err)
	}

	// Verify Firecracker binary exists
	if _, err := os.Stat(cfg.FirecrackerBinary); err != nil {
		return nil, fmt.Errorf("firecracker binary not found at %s: %w", cfg.FirecrackerBinary, err)
	}

	return &Manager{
		logger:         logger,
		firecrackerBin: cfg.FirecrackerBinary,
		workspaceDir:   cfg.WorkspaceDir,
		kernelImage:    cfg.KernelImage,
		rootfsImage:    cfg.RootFSImage,
	}, nil
}

// VM represents a running Firecracker VM instance.
type VM struct {
	ID      string
	Config  VMConfig
	cmd     *exec.Cmd
	manager *Manager
}

// Create creates a new VM with the given configuration.
func (m *Manager) Create(ctx context.Context, config VMConfig) (*VM, error) {
	m.logger.InfoContext(ctx, "creating VM",
		"vm_id", config.ID,
		"cpu_count", config.CPUCount,
		"memory_mb", config.MemoryMB,
	)

	// Create VM workspace directory
	vmDir := filepath.Join(m.workspaceDir, config.ID)
	if err := os.MkdirAll(vmDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create VM directory: %w", err)
	}

	// Set up network interface (tap device)
	if len(config.NetworkInterfaces) > 0 {
		for _, netif := range config.NetworkInterfaces {
			if err := m.createTapDevice(ctx, netif.HostDevName); err != nil {
				return nil, fmt.Errorf("failed to create tap device: %w", err)
			}
		}
	}

	vm := &VM{
		ID:      config.ID,
		Config:  config,
		manager: m,
	}

	return vm, nil
}

// Start starts the VM.
func (v *VM) Start(ctx context.Context) error {
	v.manager.logger.InfoContext(ctx, "starting VM", "vm_id", v.ID)

	// Create Firecracker configuration
	configPath := filepath.Join(v.manager.workspaceDir, v.ID, "config.json")
	if err := v.writeFirecrackerConfig(configPath); err != nil {
		return fmt.Errorf("failed to write firecracker config: %w", err)
	}

	// Start Firecracker process
	cmd := exec.CommandContext(ctx,
		v.manager.firecrackerBin,
		"--config-file", configPath,
		"--api-sock", v.Config.SocketPath,
	)

	// Set up logging
	if v.Config.LogPath != "" {
		logFile, err := os.Create(v.Config.LogPath)
		if err != nil {
			return fmt.Errorf("failed to create log file: %w", err)
		}
		cmd.Stdout = logFile
		cmd.Stderr = logFile
	}

	// Start the process
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start firecracker: %w", err)
	}

	v.cmd = cmd

	// Wait for VM to be ready
	if err := v.waitForReady(ctx, 30*time.Second); err != nil {
		v.Stop(ctx, 5*time.Second)
		return fmt.Errorf("VM failed to become ready: %w", err)
	}

	v.manager.logger.InfoContext(ctx, "VM started successfully", "vm_id", v.ID)
	return nil
}

// Stop stops the VM gracefully.
func (v *VM) Stop(ctx context.Context, timeout time.Duration) error {
	if v.cmd == nil || v.cmd.Process == nil {
		return nil
	}

	v.manager.logger.InfoContext(ctx, "stopping VM", "vm_id", v.ID, "timeout", timeout)

	// Send SIGTERM for graceful shutdown
	if err := v.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("failed to send SIGTERM: %w", err)
	}

	// Wait for process to exit or timeout
	done := make(chan error, 1)
	go func() {
		done <- v.cmd.Wait()
	}()

	select {
	case <-time.After(timeout):
		// Timeout - force kill
		v.manager.logger.WarnContext(ctx, "VM did not stop gracefully, forcing kill", "vm_id", v.ID)
		if err := v.cmd.Process.Kill(); err != nil {
			return fmt.Errorf("failed to kill VM process: %w", err)
		}
		<-done // Wait for the process to be reaped
	case err := <-done:
		if err != nil {
			v.manager.logger.WarnContext(ctx, "VM exited with error", "vm_id", v.ID, "error", err)
		}
	}

	v.manager.logger.InfoContext(ctx, "VM stopped", "vm_id", v.ID)
	return nil
}

// Destroy cleans up all VM resources.
func (v *VM) Destroy(ctx context.Context) error {
	v.manager.logger.InfoContext(ctx, "destroying VM", "vm_id", v.ID)

	// Ensure VM is stopped
	if v.cmd != nil && v.cmd.Process != nil {
		if err := v.Stop(ctx, 5*time.Second); err != nil {
			v.manager.logger.WarnContext(ctx, "error stopping VM during destroy", "vm_id", v.ID, "error", err)
		}
	}

	// Clean up tap devices
	for _, netif := range v.Config.NetworkInterfaces {
		if err := v.manager.deleteTapDevice(ctx, netif.HostDevName); err != nil {
			v.manager.logger.WarnContext(ctx, "failed to delete tap device", "device", netif.HostDevName, "error", err)
		}
	}

	// Remove VM directory
	vmDir := filepath.Join(v.manager.workspaceDir, v.ID)
	if err := os.RemoveAll(vmDir); err != nil {
		return fmt.Errorf("failed to remove VM directory: %w", err)
	}

	v.manager.logger.InfoContext(ctx, "VM destroyed", "vm_id", v.ID)
	return nil
}

// waitForReady waits for the VM to become ready.
func (v *VM) waitForReady(ctx context.Context, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for VM to be ready")
		case <-ticker.C:
			// Check if socket exists
			if _, err := os.Stat(v.Config.SocketPath); err == nil {
				return nil
			}
		}
	}
}

// writeFirecrackerConfig writes the Firecracker configuration to a file.
func (v *VM) writeFirecrackerConfig(path string) error {
	// This is a simplified version - in production, use proper JSON marshaling
	// and include all necessary Firecracker configuration options
	config := fmt.Sprintf(`{
  "boot-source": {
    "kernel_image_path": "%s",
    "boot_args": "%s"
  },
  "drives": [{
    "drive_id": "rootfs",
    "path_on_host": "%s",
    "is_root_device": true,
    "is_read_only": false
  }],
  "machine-config": {
    "vcpu_count": %d,
    "mem_size_mib": %d
  }
}`, v.Config.KernelImagePath, v.Config.BootArgs, v.Config.RootfsPath, v.Config.CPUCount, v.Config.MemoryMB)

	return os.WriteFile(path, []byte(config), 0644)
}

// createTapDevice creates a tap network device.
func (m *Manager) createTapDevice(ctx context.Context, name string) error {
	m.logger.InfoContext(ctx, "creating tap device", "device", name)

	// On Linux, use ip tuntap
	// This is a placeholder - actual implementation depends on OS
	cmd := exec.CommandContext(ctx, "ip", "tuntap", "add", "dev", name, "mode", "tap")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to create tap device: %w", err)
	}

	// Bring the interface up
	cmd = exec.CommandContext(ctx, "ip", "link", "set", name, "up")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to bring up tap device: %w", err)
	}

	return nil
}

// deleteTapDevice removes a tap network device.
func (m *Manager) deleteTapDevice(ctx context.Context, name string) error {
	m.logger.InfoContext(ctx, "deleting tap device", "device", name)

	cmd := exec.CommandContext(ctx, "ip", "tuntap", "del", "dev", name, "mode", "tap")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to delete tap device: %w", err)
	}

	return nil
}
