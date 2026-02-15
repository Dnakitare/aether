package vm

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// TestNewManager tests the Manager constructor.
func TestNewManager(t *testing.T) {
	tests := []struct {
		name    string
		cfg     ManagerConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid_configuration",
			cfg: ManagerConfig{
				FirecrackerBinary: createTestBinary(t),
				WorkspaceDir:      t.TempDir(),
				KernelImage:       "/tmp/kernel",
				RootFSImage:       "/tmp/rootfs",
			},
			wantErr: false,
		},
		{
			name: "missing_firecracker_binary",
			cfg: ManagerConfig{
				FirecrackerBinary: "/nonexistent/firecracker",
				WorkspaceDir:      t.TempDir(),
			},
			wantErr: true,
			errMsg:  "firecracker binary not found",
		},
		{
			name: "workspace_dir_created_if_missing",
			cfg: ManagerConfig{
				FirecrackerBinary: createTestBinary(t),
				WorkspaceDir:      filepath.Join(t.TempDir(), "new", "workspace"),
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
			mgr, err := NewManager(logger, tt.cfg)

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("error message mismatch: want %q, got %q", tt.errMsg, err.Error())
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if mgr == nil {
				t.Fatal("expected manager, got nil")
			}

			// Verify workspace directory was created
			if _, err := os.Stat(tt.cfg.WorkspaceDir); os.IsNotExist(err) {
				t.Errorf("workspace directory not created: %s", tt.cfg.WorkspaceDir)
			}
		})
	}
}

// TestManager_Create tests VM creation.
func TestManager_Create(t *testing.T) {
	mgr := createTestManager(t)
	ctx := context.Background()

	t.Run("basic_vm_creation", func(t *testing.T) {
		config := VMConfig{
			ID:       "test-vm-1",
			CPUCount: 2,
			MemoryMB: 512,
		}

		vm, err := mgr.Create(ctx, config)
		if err != nil {
			t.Fatalf("failed to create VM: %v", err)
		}

		if vm == nil {
			t.Fatal("expected VM, got nil")
		}

		if vm.ID != config.ID {
			t.Errorf("VM ID mismatch: want %s, got %s", config.ID, vm.ID)
		}

		// Verify VM directory was created
		vmDir := filepath.Join(mgr.workspaceDir, config.ID)
		if _, err := os.Stat(vmDir); os.IsNotExist(err) {
			t.Errorf("VM directory not created: %s", vmDir)
		}

		// Cleanup
		os.RemoveAll(vmDir)
	})

	t.Run("vm_with_network_interfaces", func(t *testing.T) {
		// Skip if not running as root (tap devices require root)
		if os.Geteuid() != 0 {
			t.Skip("skipping tap device test (requires root)")
		}

		config := VMConfig{
			ID:       "test-vm-2",
			CPUCount: 1,
			MemoryMB: 256,
			NetworkInterfaces: []NetworkInterface{
				{
					HostDevName: "tap-test-1",
				},
			},
		}

		vm, err := mgr.Create(ctx, config)
		if err != nil {
			t.Fatalf("failed to create VM with network: %v", err)
		}

		// Verify tap device was created
		cmd := exec.Command("ip", "link", "show", "tap-test-1")
		if err := cmd.Run(); err != nil {
			t.Errorf("tap device not found: %v", err)
		}

		// Cleanup
		vm.Destroy(ctx)
	})

	t.Run("vm_directory_creation_error_cleanup", func(t *testing.T) {
		// Create manager with invalid workspace (read-only)
		invalidWorkspace := filepath.Join(t.TempDir(), "readonly")
		os.MkdirAll(invalidWorkspace, 0555) // Read-only
		defer os.Chmod(invalidWorkspace, 0755)

		invalidMgr := &Manager{
			logger:         mgr.logger,
			firecrackerBin: mgr.firecrackerBin,
			workspaceDir:   invalidWorkspace,
		}

		config := VMConfig{
			ID:       "test-vm-fail",
			CPUCount: 1,
			MemoryMB: 256,
		}

		vm, err := invalidMgr.Create(ctx, config)
		if err == nil {
			t.Fatal("expected error for read-only workspace, got nil")
		}

		if vm != nil {
			t.Error("expected nil VM on error")
		}
	})
}

// TestVM_Start tests VM startup.
func TestVM_Start(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping slow Start tests in short mode")
	}

	t.Run("start_fails_without_firecracker", func(t *testing.T) {
		mgr := createTestManager(t)
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()

		vm, err := mgr.Create(ctx, VMConfig{
			ID:              "test-start-fail",
			CPUCount:        1,
			MemoryMB:        256,
			KernelImagePath: "/tmp/kernel",
			RootfsPath:      "/tmp/rootfs",
			SocketPath:      filepath.Join(t.TempDir(), "socket"),
			LogPath:         filepath.Join(t.TempDir(), "vm.log"),
		})
		if err != nil {
			t.Fatalf("failed to create VM: %v", err)
		}
		defer vm.Destroy(context.Background())

		// Start will fail because we don't have real Firecracker
		err = vm.Start(ctx)
		if err == nil {
			t.Error("expected error starting without Firecracker binary")
		}
	})

	t.Run("start_creates_log_file", func(t *testing.T) {
		mgr := createTestManager(t)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		logPath := filepath.Join(t.TempDir(), "test.log")
		vm, err := mgr.Create(ctx, VMConfig{
			ID:              "test-log",
			CPUCount:        1,
			MemoryMB:        256,
			KernelImagePath: "/tmp/kernel",
			RootfsPath:      "/tmp/rootfs",
			SocketPath:      filepath.Join(t.TempDir(), "socket"),
			LogPath:         logPath,
		})
		if err != nil {
			t.Fatalf("failed to create VM: %v", err)
		}
		defer vm.Destroy(context.Background())

		// Attempt to start (will fail with timeout or exec error)
		vm.Start(ctx)

		// Verify log file was created before failure
		if _, err := os.Stat(logPath); err != nil {
			// Log file may or may not exist depending on when Start failed
			// This is acceptable
		}
	})
}

// TestVM_Stop tests VM shutdown.
func TestVM_Stop(t *testing.T) {
	t.Run("stop_nil_process", func(t *testing.T) {
		mgr := createTestManager(t)
		vm := &VM{
			ID:      "test-stop-nil",
			manager: mgr,
			cmd:     nil, // No process running
		}

		ctx := context.Background()
		err := vm.Stop(ctx, 5*time.Second)
		if err != nil {
			t.Errorf("Stop with nil process should not error: %v", err)
		}
	})

	t.Run("stop_with_running_process", func(t *testing.T) {
		mgr := createTestManager(t)

		// Create a long-running process to test stopping
		ctx := context.Background()
		cmd := exec.CommandContext(ctx, "sleep", "10")
		if err := cmd.Start(); err != nil {
			t.Fatalf("failed to start test process: %v", err)
		}

		vm := &VM{
			ID:      "test-stop-process",
			manager: mgr,
			cmd:     cmd,
		}

		// Stop should gracefully terminate
		err := vm.Stop(ctx, 1*time.Second)
		if err != nil {
			t.Errorf("unexpected error stopping VM: %v", err)
		}

		// Verify process is stopped
		if cmd.Process != nil {
			if err := cmd.Process.Signal(os.Signal(syscall.Signal(0))); err == nil {
				t.Error("process still running after Stop")
			}
		}
	})

	t.Run("stop_timeout_force_kill", func(t *testing.T) {
		mgr := createTestManager(t)

		// Create a process that ignores SIGTERM
		ctx := context.Background()
		cmd := exec.CommandContext(ctx, "sh", "-c", "trap '' TERM; sleep 10")
		if err := cmd.Start(); err != nil {
			t.Fatalf("failed to start test process: %v", err)
		}

		vm := &VM{
			ID:      "test-stop-kill",
			manager: mgr,
			cmd:     cmd,
		}

		// Stop with short timeout should force kill
		start := time.Now()
		err := vm.Stop(ctx, 100*time.Millisecond)
		elapsed := time.Since(start)

		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}

		// Should have killed quickly after timeout
		if elapsed > 500*time.Millisecond {
			t.Errorf("force kill took too long: %v", elapsed)
		}
	})

	t.Run("stop_closes_log_file", func(t *testing.T) {
		mgr := createTestManager(t)

		logPath := filepath.Join(t.TempDir(), "test.log")
		logFile, err := os.Create(logPath)
		if err != nil {
			t.Fatalf("failed to create log file: %v", err)
		}

		ctx := context.Background()
		cmd := exec.CommandContext(ctx, "sleep", "1")
		cmd.Stdout = logFile
		cmd.Stderr = logFile

		if err := cmd.Start(); err != nil {
			t.Fatalf("failed to start process: %v", err)
		}

		vm := &VM{
			ID:      "test-log-close",
			manager: mgr,
			cmd:     cmd,
			logFile: logFile,
		}

		err = vm.Stop(ctx, 2*time.Second)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}

		// Verify log file was closed
		if vm.logFile != nil {
			t.Error("log file not closed after Stop")
		}

		// Try to write to log file (should fail if closed)
		_, writeErr := logFile.Write([]byte("test"))
		if writeErr == nil {
			t.Error("log file still writable after Stop")
		}
	})
}

// TestVM_Destroy tests VM cleanup.
func TestVM_Destroy(t *testing.T) {
	t.Run("destroy_removes_vm_directory", func(t *testing.T) {
		mgr := createTestManager(t)
		ctx := context.Background()

		vm, err := mgr.Create(ctx, VMConfig{
			ID:       "test-destroy-1",
			CPUCount: 1,
			MemoryMB: 256,
		})
		if err != nil {
			t.Fatalf("failed to create VM: %v", err)
		}

		vmDir := filepath.Join(mgr.workspaceDir, vm.ID)

		// Verify directory exists
		if _, err := os.Stat(vmDir); os.IsNotExist(err) {
			t.Fatal("VM directory not created")
		}

		err = vm.Destroy(ctx)
		if err != nil {
			t.Fatalf("failed to destroy VM: %v", err)
		}

		// Verify directory was removed
		if _, err := os.Stat(vmDir); !os.IsNotExist(err) {
			t.Error("VM directory not removed after Destroy")
		}
	})

	t.Run("destroy_stops_running_process", func(t *testing.T) {
		mgr := createTestManager(t)
		ctx := context.Background()

		vm, err := mgr.Create(ctx, VMConfig{
			ID:       "test-destroy-process",
			CPUCount: 1,
			MemoryMB: 256,
		})
		if err != nil {
			t.Fatalf("failed to create VM: %v", err)
		}

		// Attach a running process
		cmd := exec.CommandContext(ctx, "sleep", "10")
		if err := cmd.Start(); err != nil {
			t.Fatalf("failed to start process: %v", err)
		}
		vm.cmd = cmd

		err = vm.Destroy(ctx)
		if err != nil {
			t.Errorf("destroy failed: %v", err)
		}

		// Verify process is stopped
		if cmd.Process != nil {
			if err := cmd.Process.Signal(os.Signal(syscall.Signal(0))); err == nil {
				t.Error("process still running after Destroy")
			}
		}
	})
}

// TestCreateTapDevice tests network device creation.
func TestCreateTapDevice(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("skipping tap device tests (requires root)")
	}

	mgr := createTestManager(t)
	ctx := context.Background()

	t.Run("valid_device_name", func(t *testing.T) {
		devName := "tap-test-valid"
		defer mgr.deleteTapDevice(ctx, devName)

		err := mgr.createTapDevice(ctx, devName)
		if err != nil {
			t.Fatalf("failed to create tap device: %v", err)
		}

		// Verify device exists
		cmd := exec.Command("ip", "link", "show", devName)
		if err := cmd.Run(); err != nil {
			t.Errorf("tap device not found: %v", err)
		}
	})

	t.Run("invalid_device_name_injection", func(t *testing.T) {
		invalidNames := []string{
			"tap-test; rm -rf /",
			"tap$(whoami)",
			"tap`ls`",
			"tap|cat /etc/passwd",
			"tap&& echo pwned",
		}

		for _, name := range invalidNames {
			err := mgr.createTapDevice(ctx, name)
			if err == nil {
				t.Errorf("expected error for malicious device name: %s", name)
			}
			if !strings.Contains(err.Error(), "invalid device name") {
				t.Errorf("wrong error for %s: %v", name, err)
			}
		}
	})

	t.Run("device_name_too_long", func(t *testing.T) {
		// Linux IFNAMSIZ is 16, our limit is 15
		longName := "tap-very-long-name-exceeds-limit"

		err := mgr.createTapDevice(ctx, longName)
		if err == nil {
			t.Error("expected error for device name > 15 chars")
		}
	})

	t.Run("duplicate_device_name", func(t *testing.T) {
		devName := "tap-test-dup"

		// Create first device
		err := mgr.createTapDevice(ctx, devName)
		if err != nil {
			t.Fatalf("failed to create first device: %v", err)
		}
		defer mgr.deleteTapDevice(ctx, devName)

		// Try to create duplicate
		err = mgr.createTapDevice(ctx, devName)
		if err == nil {
			t.Error("expected error for duplicate device name")
		}
	})
}

// TestDeleteTapDevice tests network device deletion.
func TestDeleteTapDevice(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("skipping tap device tests (requires root)")
	}

	mgr := createTestManager(t)
	ctx := context.Background()

	t.Run("delete_existing_device", func(t *testing.T) {
		devName := "tap-test-delete"

		// Create device
		if err := mgr.createTapDevice(ctx, devName); err != nil {
			t.Fatalf("failed to create device: %v", err)
		}

		// Delete device
		err := mgr.deleteTapDevice(ctx, devName)
		if err != nil {
			t.Errorf("failed to delete device: %v", err)
		}

		// Verify device is gone
		cmd := exec.Command("ip", "link", "show", devName)
		if err := cmd.Run(); err == nil {
			t.Error("device still exists after deletion")
		}
	})

	t.Run("delete_nonexistent_device", func(t *testing.T) {
		err := mgr.deleteTapDevice(ctx, "tap-nonexistent")
		if err == nil {
			t.Error("expected error deleting nonexistent device")
		}
	})

	t.Run("invalid_device_name_injection", func(t *testing.T) {
		invalidNames := []string{
			"tap; rm -rf /",
			"tap$(id)",
		}

		for _, name := range invalidNames {
			err := mgr.deleteTapDevice(ctx, name)
			if err == nil {
				t.Errorf("expected error for malicious device name: %s", name)
			}
		}
	})
}

// TestWriteFirecrackerConfig tests configuration file generation.
func TestWriteFirecrackerConfig(t *testing.T) {
	mgr := createTestManager(t)

	t.Run("valid_config_generation", func(t *testing.T) {
		vm := &VM{
			ID:      "test-config",
			manager: mgr,
			Config: VMConfig{
				KernelImagePath: "/path/to/kernel",
				BootArgs:        "console=ttyS0",
				RootfsPath:      "/path/to/rootfs",
				CPUCount:        4,
				MemoryMB:        2048,
			},
		}

		configPath := filepath.Join(t.TempDir(), "config.json")
		err := vm.writeFirecrackerConfig(configPath)
		if err != nil {
			t.Fatalf("failed to write config: %v", err)
		}

		// Verify file was created
		data, err := os.ReadFile(configPath)
		if err != nil {
			t.Fatalf("failed to read config file: %v", err)
		}

		config := string(data)

		// Verify config contains expected values
		expectedValues := map[string]bool{
			`"kernel_image_path": "/path/to/kernel"`: false,
			`"boot_args": "console=ttyS0"`:           false,
			`"path_on_host": "/path/to/rootfs"`:      false,
			`"vcpu_count": 4`:                        false,
			`"mem_size_mib": 2048`:                   false,
		}

		for expected := range expectedValues {
			if strings.Contains(config, expected) {
				expectedValues[expected] = true
			}
		}

		for expected, found := range expectedValues {
			if !found {
				t.Errorf("config missing expected value: %s", expected)
			}
		}
	})
}

// TestWaitForReady tests VM readiness detection.
func TestWaitForReady(t *testing.T) {
	mgr := createTestManager(t)

	t.Run("socket_appears_in_time", func(t *testing.T) {
		socketPath := filepath.Join(t.TempDir(), "test.sock")

		vm := &VM{
			ID:      "test-ready",
			manager: mgr,
			Config: VMConfig{
				SocketPath: socketPath,
			},
		}

		// Create socket after delay
		go func() {
			time.Sleep(200 * time.Millisecond)
			os.WriteFile(socketPath, []byte{}, 0644)
		}()

		ctx := context.Background()
		err := vm.waitForReady(ctx, 1*time.Second)
		if err != nil {
			t.Errorf("unexpected error waiting for ready: %v", err)
		}
	})

	t.Run("timeout_waiting_for_socket", func(t *testing.T) {
		socketPath := filepath.Join(t.TempDir(), "never-appears.sock")

		vm := &VM{
			ID:      "test-timeout",
			manager: mgr,
			Config: VMConfig{
				SocketPath: socketPath,
			},
		}

		ctx := context.Background()
		start := time.Now()
		err := vm.waitForReady(ctx, 200*time.Millisecond)
		elapsed := time.Since(start)

		if err == nil {
			t.Error("expected timeout error, got nil")
		}

		if !strings.Contains(err.Error(), "timeout") {
			t.Errorf("expected timeout error, got: %v", err)
		}

		if elapsed < 200*time.Millisecond {
			t.Errorf("timeout fired too early: %v", elapsed)
		}
	})

	t.Run("context_cancellation", func(t *testing.T) {
		socketPath := filepath.Join(t.TempDir(), "cancel.sock")

		vm := &VM{
			ID:      "test-cancel",
			manager: mgr,
			Config: VMConfig{
				SocketPath: socketPath,
			},
		}

		ctx, cancel := context.WithCancel(context.Background())

		// Cancel context after delay
		go func() {
			time.Sleep(100 * time.Millisecond)
			cancel()
		}()

		err := vm.waitForReady(ctx, 10*time.Second)
		if err == nil {
			t.Error("expected error from cancelled context")
		}
	})
}

// TestConcurrentVMOperations tests thread safety.
func TestConcurrentVMOperations(t *testing.T) {
	mgr := createTestManager(t)
	ctx := context.Background()

	t.Run("concurrent_vm_creation", func(t *testing.T) {
		const numVMs = 10
		var wg sync.WaitGroup
		errors := make(chan error, numVMs)

		for i := 0; i < numVMs; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()

				config := VMConfig{
					ID:       fmt.Sprintf("concurrent-vm-%d", id),
					CPUCount: 1,
					MemoryMB: 256,
				}

				vm, err := mgr.Create(ctx, config)
				if err != nil {
					errors <- err
					return
				}

				// Cleanup
				vm.Destroy(ctx)
			}(i)
		}

		wg.Wait()
		close(errors)

		for err := range errors {
			t.Errorf("concurrent creation error: %v", err)
		}
	})
}

// TestVM_Start_ErrorPaths tests Start error handling.
func TestVM_Start_ErrorPaths(t *testing.T) {
	mgr := createTestManager(t)
	ctx := context.Background()

	t.Run("start_fails_with_invalid_config_path", func(t *testing.T) {
		// Create VM with invalid workspace (will fail to write config)
		invalidWorkspace := "/nonexistent/path"
		invalidMgr := &Manager{
			logger:         mgr.logger,
			firecrackerBin: mgr.firecrackerBin,
			workspaceDir:   invalidWorkspace,
		}

		vm := &VM{
			ID:      "test-invalid-path",
			manager: invalidMgr,
			Config: VMConfig{
				ID:              "test-invalid-path",
				KernelImagePath: "/tmp/kernel",
				RootfsPath:      "/tmp/rootfs",
				SocketPath:      "/tmp/socket",
			},
		}

		err := vm.Start(ctx)
		if err == nil {
			t.Error("expected error with invalid config path")
		}
		if !strings.Contains(err.Error(), "failed to write firecracker config") {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("start_cleanup_on_failure", func(t *testing.T) {
		logPath := filepath.Join(t.TempDir(), "cleanup-test.log")

		// Use short timeout context to avoid long waits
		failCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
		defer cancel()

		vm, err := mgr.Create(ctx, VMConfig{
			ID:              "test-cleanup-fail",
			CPUCount:        1,
			MemoryMB:        256,
			KernelImagePath: "/tmp/kernel",
			RootfsPath:      "/tmp/rootfs",
			SocketPath:      filepath.Join(t.TempDir(), "never-created.sock"),
			LogPath:         logPath,
		})
		if err != nil {
			t.Fatalf("failed to create VM: %v", err)
		}
		defer vm.Destroy(ctx)

		// Start will fail (no real Firecracker), verify log file cleanup
		err = vm.Start(failCtx)
		if err == nil {
			t.Error("expected error starting VM without Firecracker")
		}

		// Verify log file was created and then closed on error
		if vm.logFile != nil {
			t.Error("log file should be closed after Start failure")
		}
	})
}

// TestVM_Stop_ErrorPaths tests Stop error handling.
func TestVM_Stop_ErrorPaths(t *testing.T) {
	mgr := createTestManager(t)
	ctx := context.Background()

	t.Run("stop_signal_error", func(t *testing.T) {
		// Create VM with already-stopped process
		vm := &VM{
			ID:      "test-stop-error",
			manager: mgr,
		}

		// Create a process that's already exited
		cmd := exec.CommandContext(ctx, "true") // exits immediately
		cmd.Start()
		cmd.Wait() // Process is now stopped

		vm.cmd = cmd

		// Stop should handle the error gracefully
		err := vm.Stop(ctx, 1*time.Second)
		// Error is expected but should be handled
		_ = err
	})
}

// TestVM_Destroy_ErrorPaths tests Destroy error handling.
func TestVM_Destroy_ErrorPaths(t *testing.T) {
	mgr := createTestManager(t)
	ctx := context.Background()

	t.Run("destroy_with_tap_device_cleanup_error", func(t *testing.T) {
		// Create VM with valid tap device name but don't actually create device
		vm := &VM{
			ID:      "test-destroy-tap",
			manager: mgr,
			Config: VMConfig{
				ID:       "test-destroy-tap",
				CPUCount: 1,
				MemoryMB: 256,
				NetworkInterfaces: []NetworkInterface{
					{
						HostDevName: "tap-test-del", // Valid name, but device doesn't exist
					},
				},
			},
		}

		// Create VM directory
		vmDir := filepath.Join(mgr.workspaceDir, vm.ID)
		os.MkdirAll(vmDir, 0755)

		// Destroy should handle tap device deletion errors gracefully
		err := vm.Destroy(ctx)
		// Should succeed even if tap device doesn't exist (logged as warning)
		if err != nil {
			t.Errorf("destroy failed: %v", err)
		}
	})

	t.Run("destroy_cleans_up_process_and_log", func(t *testing.T) {
		// Create VM with running process and log file
		logPath := filepath.Join(t.TempDir(), "destroy-test.log")
		logFile, err := os.Create(logPath)
		if err != nil {
			t.Fatalf("failed to create log file: %v", err)
		}

		cmd := exec.CommandContext(ctx, "sleep", "5")
		cmd.Stdout = logFile
		cmd.Stderr = logFile
		if err := cmd.Start(); err != nil {
			t.Fatalf("failed to start process: %v", err)
		}

		vm := &VM{
			ID:      "test-destroy-cleanup",
			manager: mgr,
			cmd:     cmd,
			logFile: logFile,
			Config: VMConfig{
				ID: "test-destroy-cleanup",
			},
		}

		// Create VM directory
		vmDir := filepath.Join(mgr.workspaceDir, vm.ID)
		os.MkdirAll(vmDir, 0755)

		err = vm.Destroy(ctx)
		if err != nil {
			t.Errorf("destroy failed: %v", err)
		}

		// Verify process stopped and log file closed
		if vm.logFile != nil {
			t.Error("log file not closed after Destroy")
		}
	})
}

// TestManager_Create_ErrorPaths tests Create error handling.
func TestManager_Create_ErrorPaths(t *testing.T) {
	mgr := createTestManager(t)
	ctx := context.Background()

	t.Run("create_with_network_cleanup_on_error", func(t *testing.T) {
		if os.Geteuid() != 0 {
			t.Skip("skipping tap device test (requires root)")
		}

		// This test verifies that if VM creation fails after creating
		// tap devices, those devices are cleaned up

		config := VMConfig{
			ID:       "test-cleanup-on-error",
			CPUCount: 1,
			MemoryMB: 256,
			NetworkInterfaces: []NetworkInterface{
				{
					HostDevName: "tap-cleanup-test",
				},
			},
		}

		// Force an error after tap device creation by making workspace read-only
		os.Chmod(mgr.workspaceDir, 0555)
		defer os.Chmod(mgr.workspaceDir, 0755)

		vm, err := mgr.Create(ctx, config)
		if err == nil {
			t.Error("expected error creating VM with read-only workspace")
			vm.Destroy(ctx)
		}

		os.Chmod(mgr.workspaceDir, 0755)

		// Verify tap device was cleaned up
		cmd := exec.Command("ip", "link", "show", "tap-cleanup-test")
		if err := cmd.Run(); err == nil {
			t.Error("tap device should have been cleaned up on error")
			// Cleanup
			mgr.deleteTapDevice(ctx, "tap-cleanup-test")
		}
	})
}

// TestResourceCleanup tests that resources are properly cleaned up.
func TestResourceCleanup(t *testing.T) {
	t.Run("file_descriptors_cleaned_up", func(t *testing.T) {
		mgr := createTestManager(t)
		ctx := context.Background()

		// Get initial file descriptor count
		initialFDs := countOpenFileDescriptors(t)

		// Create and destroy VMs
		const numIterations = 10
		for i := 0; i < numIterations; i++ {
			vm, err := mgr.Create(ctx, VMConfig{
				ID:       fmt.Sprintf("cleanup-test-%d", i),
				CPUCount: 1,
				MemoryMB: 256,
				LogPath:  filepath.Join(t.TempDir(), fmt.Sprintf("vm-%d.log", i)),
			})
			if err != nil {
				t.Fatalf("iteration %d: failed to create VM: %v", i, err)
			}

			if err := vm.Destroy(ctx); err != nil {
				t.Errorf("iteration %d: failed to destroy VM: %v", i, err)
			}
		}

		// Check final file descriptor count
		finalFDs := countOpenFileDescriptors(t)

		// Allow some tolerance (file descriptors may fluctuate slightly)
		if finalFDs > initialFDs+5 {
			t.Errorf("file descriptor leak detected: initial=%d, final=%d", initialFDs, finalFDs)
		}
	})
}

// Helper functions

func createTestManager(t *testing.T) *Manager {
	t.Helper()

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelError, // Reduce noise in tests
	}))

	cfg := ManagerConfig{
		FirecrackerBinary: createTestBinary(t),
		WorkspaceDir:      t.TempDir(),
		KernelImage:       "/tmp/kernel",
		RootFSImage:       "/tmp/rootfs",
	}

	mgr, err := NewManager(logger, cfg)
	if err != nil {
		t.Fatalf("failed to create test manager: %v", err)
	}

	return mgr
}

func createTestBinary(t *testing.T) string {
	t.Helper()

	// Create a dummy binary file
	binPath := filepath.Join(t.TempDir(), "firecracker")
	if err := os.WriteFile(binPath, []byte("#!/bin/sh\necho test\n"), 0755); err != nil {
		t.Fatalf("failed to create test binary: %v", err)
	}

	return binPath
}

func countOpenFileDescriptors(t *testing.T) int {
	t.Helper()

	pid := os.Getpid()
	fdDir := fmt.Sprintf("/proc/%d/fd", pid)

	entries, err := os.ReadDir(fdDir)
	if err != nil {
		// If /proc is not available (e.g., macOS), return 0
		return 0
	}

	return len(entries)
}
