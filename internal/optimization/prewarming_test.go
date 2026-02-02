package optimization_test

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/aether-runtime/aether/internal/optimization"
)

func TestPrewarmingConfig(t *testing.T) {
	config := optimization.DefaultPrewarmingConfig()

	if !config.Enabled {
		t.Error("Expected prewarming to be enabled by default")
	}

	if len(config.PoolSize) == 0 {
		t.Error("Expected PoolSize to be set")
	}

	if config.RefillThreshold == 0 {
		t.Error("Expected RefillThreshold to be set")
	}

	if config.MaxIdleTime == 0 {
		t.Error("Expected MaxIdleTime to be set")
	}

	if config.WarmupTime == 0 {
		t.Error("Expected WarmupTime to be set")
	}
}

func TestPrewarmingPool(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	config := optimization.DefaultPrewarmingConfig()
	// Speed up tests
	config.PoolSize = map[optimization.WorkloadType]int{
		optimization.WorkloadCodeExecution: 2,
	}
	config.WarmupTime = 100 * time.Millisecond

	pool := optimization.NewPrewarmingPool(logger, config)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Start pool
	if err := pool.Start(ctx); err != nil {
		t.Fatalf("Failed to start pool: %v", err)
	}

	// Wait for pool to fill
	time.Sleep(500 * time.Millisecond)

	// Check metrics
	metrics := pool.GetMetrics()
	codeExecMetrics := metrics[optimization.WorkloadCodeExecution]

	if codeExecMetrics.TotalCreated == 0 {
		t.Error("Expected VMs to be created")
	}
}

func TestAcquireReleaseVM(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	config := optimization.DefaultPrewarmingConfig()
	config.PoolSize = map[optimization.WorkloadType]int{
		optimization.WorkloadCodeExecution: 2,
	}
	config.WarmupTime = 100 * time.Millisecond

	pool := optimization.NewPrewarmingPool(logger, config)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := pool.Start(ctx); err != nil {
		t.Fatalf("Failed to start pool: %v", err)
	}

	// Wait for pool to fill
	time.Sleep(500 * time.Millisecond)

	// Acquire VM
	vm, err := pool.AcquireVM(ctx, optimization.WorkloadCodeExecution)
	if err != nil {
		t.Fatalf("Failed to acquire VM: %v", err)
	}

	if vm.ID == "" {
		t.Error("Expected VM ID to be set")
	}

	if !vm.InUse {
		t.Error("Expected VM to be marked as in use")
	}

	// Release VM
	if err := pool.ReleaseVM(ctx, vm.ID); err != nil {
		t.Fatalf("Failed to release VM: %v", err)
	}
}

func TestConnectionPoolConfig(t *testing.T) {
	config := optimization.DefaultConnectionPoolConfig()

	if config.MaxIdleConns == 0 {
		t.Error("Expected MaxIdleConns to be set")
	}

	if config.MaxIdleConnsPerHost == 0 {
		t.Error("Expected MaxIdleConnsPerHost to be set")
	}

	if config.MaxConnsPerHost == 0 {
		t.Error("Expected MaxConnsPerHost to be set")
	}

	if config.IdleConnTimeout == 0 {
		t.Error("Expected IdleConnTimeout to be set")
	}
}

func TestConnectionPool(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	config := optimization.DefaultConnectionPoolConfig()
	pool := optimization.NewConnectionPool(logger, config)

	// Get client
	client := pool.GetClient(optimization.ProviderOpenAI)
	if client == nil {
		t.Error("Expected non-nil client")
	}

	// Get same client again (should reuse)
	client2 := pool.GetClient(optimization.ProviderOpenAI)
	if client != client2 {
		t.Error("Expected same client instance")
	}

	// Get different provider
	client3 := pool.GetClient(optimization.ProviderAnthropic)
	if client3 == client {
		t.Error("Expected different client for different provider")
	}

	// Check metrics
	metrics := pool.GetMetrics()
	if len(metrics) == 0 {
		t.Error("Expected metrics to be tracked")
	}
}

func TestWorkloadProfiles(t *testing.T) {
	workloadTypes := []optimization.WorkloadType{
		optimization.WorkloadCodeExecution,
		optimization.WorkloadLLMAgent,
		optimization.WorkloadDataProcessing,
		optimization.WorkloadLongRunning,
	}

	for _, wt := range workloadTypes {
		profile := optimization.GetWorkloadProfile(wt)

		if profile.Name != wt {
			t.Errorf("Expected profile name = %s, got %s", wt, profile.Name)
		}

		if profile.Description == "" {
			t.Errorf("Expected description for %s", wt)
		}

		if profile.CPULimit == 0 {
			t.Errorf("Expected CPULimit for %s", wt)
		}

		if profile.MemoryLimit == 0 {
			t.Errorf("Expected MemoryLimit for %s", wt)
		}
	}
}

func TestWorkloadProfileDefaults(t *testing.T) {
	// Code execution should not have checkpointing
	codeExec := optimization.GetWorkloadProfile(optimization.WorkloadCodeExecution)
	if codeExec.EnableCheckpointing {
		t.Error("Code execution should not have checkpointing enabled")
	}

	// LLM agents should have keep-alive
	llmAgent := optimization.GetWorkloadProfile(optimization.WorkloadLLMAgent)
	if !llmAgent.KeepAlive {
		t.Error("LLM agents should have keep-alive enabled")
	}

	// Data processing should have high memory
	dataProc := optimization.GetWorkloadProfile(optimization.WorkloadDataProcessing)
	if dataProc.MemoryLimit < 10000 {
		t.Error("Data processing should have high memory limit")
	}

	// Long-running should have frequent checkpoints
	longRun := optimization.GetWorkloadProfile(optimization.WorkloadLongRunning)
	if !longRun.EnableCheckpointing {
		t.Error("Long-running should have checkpointing enabled")
	}
	if longRun.CheckpointRetention < 5 {
		t.Error("Long-running should have high checkpoint retention")
	}
}

func TestOptimizationManager(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	prewarmingConfig := optimization.DefaultPrewarmingConfig()
	prewarmingConfig.Enabled = false // Disable for quick test

	connectionPoolConfig := optimization.DefaultConnectionPoolConfig()

	manager := optimization.NewOptimizationManager(logger, prewarmingConfig, connectionPoolConfig)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := manager.Start(ctx); err != nil {
		t.Fatalf("Failed to start optimization manager: %v", err)
	}

	// Get components
	prewarmingPool := manager.GetPrewarmingPool()
	if prewarmingPool == nil {
		t.Error("Expected non-nil prewarming pool")
	}

	connectionPool := manager.GetConnectionPool()
	if connectionPool == nil {
		t.Error("Expected non-nil connection pool")
	}

	// Get metrics
	metrics := manager.GetMetrics()
	if metrics == nil {
		t.Error("Expected non-nil metrics")
	}

	// Close
	manager.Close()
}
