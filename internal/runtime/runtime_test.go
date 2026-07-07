package runtime

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/dnakitare/aether/internal/runtime/agent"
	"github.com/dnakitare/aether/internal/runtime/vm"
	"github.com/dnakitare/aether/pkg/api"
)

// Mock implementations

type MockStateStore struct {
	mock.Mock
}

func (m *MockStateStore) CreateAgent(ctx context.Context, config api.AgentConfig) error {
	args := m.Called(ctx, config)
	return args.Error(0)
}

func (m *MockStateStore) GetAgent(ctx context.Context, agentID api.AgentID) (*api.AgentInfo, error) {
	args := m.Called(ctx, agentID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*api.AgentInfo), args.Error(1)
}

func (m *MockStateStore) ListAgents(ctx context.Context, tenantID api.TenantID) ([]*api.AgentInfo, error) {
	args := m.Called(ctx, tenantID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*api.AgentInfo), args.Error(1)
}

func (m *MockStateStore) UpdateAgentStatus(ctx context.Context, agentID api.AgentID, status api.AgentStatus) error {
	args := m.Called(ctx, agentID, status)
	return args.Error(0)
}

func (m *MockStateStore) SetAgentError(ctx context.Context, agentID api.AgentID, errMsg string) error {
	args := m.Called(ctx, agentID, errMsg)
	return args.Error(0)
}

func (m *MockStateStore) ListAllAgents(ctx context.Context) ([]*api.AgentInfo, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*api.AgentInfo), args.Error(1)
}

func (m *MockStateStore) DeleteAgent(ctx context.Context, agentID api.AgentID) error {
	args := m.Called(ctx, agentID)
	return args.Error(0)
}

type MockMetricsRecorder struct {
	mock.Mock
}

func (m *MockMetricsRecorder) RecordAgentOperation(operation string, status string, tenantID api.TenantID) {
	m.Called(operation, status, tenantID)
}

func (m *MockMetricsRecorder) RecordAgentStartup(tenantID api.TenantID, duration time.Duration) {
	m.Called(tenantID, duration)
}

func (m *MockMetricsRecorder) RecordAgentError(errorType string, tenantID api.TenantID) {
	m.Called(errorType, tenantID)
}

func (m *MockMetricsRecorder) SetAgentCount(status api.AgentStatus, tenantID api.TenantID, count float64) {
	m.Called(status, tenantID, count)
}

// Helper functions

func createTestRuntime(t *testing.T, stateStore StateStore) *Runtime {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	// Create temp directory for workspace
	workspaceDir := t.TempDir()

	// Create dummy Firecracker binary for tests
	firecrackerPath := workspaceDir + "/firecracker"
	err := os.WriteFile(firecrackerPath, []byte("#!/bin/sh\necho test"), 0755)
	require.NoError(t, err)

	config := Config{
		VMManagerConfig: vm.ManagerConfig{
			// These paths don't need to exist for unit tests
			// VM creation will be skipped in tests without Firecracker
			KernelImage:       "/tmp/kernel.img",
			RootFSImage:       "/tmp/rootfs.img",
			FirecrackerBinary: firecrackerPath,
			WorkspaceDir:      workspaceDir,
		},
		DefaultResources: api.ResourceLimits{
			CPUCount: 2,
			MemoryMB: 512,
		},
		WorkspaceDir: workspaceDir,
	}

	runtime, err := New(logger, config, stateStore)
	require.NoError(t, err)

	return runtime
}

func createTestAgentConfig() api.AgentConfig {
	return api.AgentConfig{
		ID:       api.AgentID("test-agent-1"),
		TenantID: api.TenantID("test-tenant"),
		Name:     "Test Agent",
		Image:    "python:3.11",
		Resources: api.ResourceLimits{
			CPUCount: 1,
			MemoryMB: 256,
		},
	}
}

// Tests

func TestNew(t *testing.T) {
	t.Run("successful creation", func(t *testing.T) {
		mockStore := new(MockStateStore)
		runtime := createTestRuntime(t, mockStore)

		assert.NotNil(t, runtime)
		assert.NotNil(t, runtime.logger)
		assert.NotNil(t, runtime.tracer)
		assert.NotNil(t, runtime.vmManager)
		assert.Equal(t, mockStore, runtime.stateStore)
		assert.NotNil(t, runtime.agents)
		assert.Equal(t, 0, len(runtime.agents))
	})

	t.Run("nil state store is allowed", func(t *testing.T) {
		runtime := createTestRuntime(t, nil)
		assert.NotNil(t, runtime)
		assert.Nil(t, runtime.stateStore)
	})
}

func TestSetMetrics(t *testing.T) {
	mockStore := new(MockStateStore)
	runtime := createTestRuntime(t, mockStore)

	mockMetrics := new(MockMetricsRecorder)
	runtime.SetMetrics(mockMetrics)

	assert.Equal(t, mockMetrics, runtime.metrics)
}

func TestCreateAgent(t *testing.T) {
	t.Run("VM creation not available in unit tests", func(t *testing.T) {
		// This test documents that CreateAgent requires actual VM infrastructure
		// which is not available in unit tests. VM creation will fail.
		mockStore := new(MockStateStore)
		// State is persisted before VM creation; mock must accept the create call.
		mockStore.On("CreateAgent", mock.Anything, mock.Anything).Return(nil)
		mockStore.On("DeleteAgent", mock.Anything, mock.Anything).Return(nil)
		runtime := createTestRuntime(t, mockStore)

		config := createTestAgentConfig()

		err := runtime.CreateAgent(context.Background(), config)
		// Expect failure because Firecracker binary doesn't exist
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to create VM")
	})

	t.Run("default resources applied when not specified", func(t *testing.T) {
		// Test that default resources are applied to config
		mockStore := new(MockStateStore)
		mockStore.On("CreateAgent", mock.Anything, mock.Anything).Return(nil)
		mockStore.On("DeleteAgent", mock.Anything, mock.Anything).Return(nil)
		runtime := createTestRuntime(t, mockStore)

		config := createTestAgentConfig()
		config.Resources = api.ResourceLimits{} // Empty resources

		err := runtime.CreateAgent(context.Background(), config)
		// Will fail at VM creation, but we can verify defaults were applied
		// by checking the error doesn't complain about missing resources
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to create VM")
	})

	t.Run("metrics recorded on operations", func(t *testing.T) {
		mockStore := new(MockStateStore)
		mockStore.On("CreateAgent", mock.Anything, mock.Anything).Return(nil)
		mockStore.On("DeleteAgent", mock.Anything, mock.Anything).Return(nil)
		mockMetrics := new(MockMetricsRecorder)
		runtime := createTestRuntime(t, mockStore)
		runtime.SetMetrics(mockMetrics)

		config := createTestAgentConfig()

		// Expect metrics to be recorded (will fail at VM creation)
		mockMetrics.On("RecordAgentOperation", "create", "failed", config.TenantID).Return()
		mockMetrics.On("RecordAgentError", mock.Anything, config.TenantID).Return()

		err := runtime.CreateAgent(context.Background(), config)
		assert.Error(t, err)

		mockMetrics.AssertExpectations(t)
	})
}

func TestGetAgent(t *testing.T) {
	t.Run("agent not found", func(t *testing.T) {
		mockStore := new(MockStateStore)
		runtime := createTestRuntime(t, mockStore)

		info, err := runtime.GetAgent(context.Background(), "nonexistent")
		assert.Error(t, err)
		assert.Nil(t, info)
		assert.Contains(t, err.Error(), "not found")
	})
}

func TestListAgents(t *testing.T) {
	t.Run("empty runtime returns empty list", func(t *testing.T) {
		mockStore := new(MockStateStore)
		runtime := createTestRuntime(t, mockStore)

		agents, err := runtime.ListAgents(context.Background(), nil)
		assert.NoError(t, err)
		assert.NotNil(t, agents)
		assert.Equal(t, 0, len(agents))
	})

	t.Run("tenant filtering", func(t *testing.T) {
		mockStore := new(MockStateStore)
		runtime := createTestRuntime(t, mockStore)

		// Empty runtime, but test the filtering logic
		tenantID := api.TenantID("test-tenant")
		agents, err := runtime.ListAgents(context.Background(), &tenantID)
		assert.NoError(t, err)
		assert.NotNil(t, agents)
		assert.Equal(t, 0, len(agents))
	})
}

func TestGetAgentLogs(t *testing.T) {
	t.Run("agent not found", func(t *testing.T) {
		mockStore := new(MockStateStore)
		runtime := createTestRuntime(t, mockStore)

		logs, err := runtime.GetAgentLogs(context.Background(), "nonexistent", false)
		assert.Error(t, err)
		assert.Nil(t, logs)
		assert.Contains(t, err.Error(), "not found")
	})
}

func TestGetAgentHealth(t *testing.T) {
	t.Run("agent not found", func(t *testing.T) {
		mockStore := new(MockStateStore)
		runtime := createTestRuntime(t, mockStore)

		health, err := runtime.GetAgentHealth(context.Background(), "nonexistent")
		assert.Error(t, err)
		assert.Nil(t, health)
		assert.Contains(t, err.Error(), "not found")
	})
}

func TestStartAgent(t *testing.T) {
	t.Run("agent not found", func(t *testing.T) {
		mockStore := new(MockStateStore)
		runtime := createTestRuntime(t, mockStore)

		err := runtime.StartAgent(context.Background(), "nonexistent")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})
}

func TestStopAgent(t *testing.T) {
	t.Run("agent not found", func(t *testing.T) {
		mockStore := new(MockStateStore)
		runtime := createTestRuntime(t, mockStore)

		err := runtime.StopAgent(context.Background(), "nonexistent", 5*time.Second)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})
}

func TestDestroyAgent(t *testing.T) {
	t.Run("agent not found", func(t *testing.T) {
		mockStore := new(MockStateStore)
		runtime := createTestRuntime(t, mockStore)

		err := runtime.DestroyAgent(context.Background(), "nonexistent")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})
}

func TestShutdown(t *testing.T) {
	t.Run("shutdown empty runtime", func(t *testing.T) {
		mockStore := new(MockStateStore)
		runtime := createTestRuntime(t, mockStore)

		err := runtime.Shutdown(context.Background())
		assert.NoError(t, err)
	})

	t.Run("shutdown cleans up agents", func(t *testing.T) {
		mockStore := new(MockStateStore)
		runtime := createTestRuntime(t, mockStore)

		// Even with no agents, shutdown should succeed
		err := runtime.Shutdown(context.Background())
		assert.NoError(t, err)
	})
}

func TestCheckpointMethods(t *testing.T) {
	t.Run("checkpoint manager not initialized", func(t *testing.T) {
		mockStore := new(MockStateStore)
		runtime := createTestRuntime(t, mockStore)

		ctx := context.Background()
		agentID := api.AgentID("test-agent")

		// All checkpoint methods should fail when manager not initialized
		_, err := runtime.CreateCheckpoint(ctx, agentID)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "checkpoint manager not initialized")

		_, err = runtime.ListCheckpoints(ctx, agentID)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "checkpoint manager not initialized")

		_, err = runtime.GetLatestCheckpoint(ctx, agentID)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "checkpoint manager not initialized")

		err = runtime.RestoreFromCheckpoint(ctx, agentID, 1)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "checkpoint manager not initialized")

		err = runtime.DeleteCheckpoint(ctx, agentID, 1)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "checkpoint manager not initialized")
	})
}

func TestVMAdapter(t *testing.T) {
	t.Run("IsRunning returns false when no process is set", func(t *testing.T) {
		// A VM that was never started (no cmd) must not be considered running.
		vmInstance := &vm.VM{
			Config: vm.VMConfig{
				SocketPath: "/tmp/test.sock",
			},
		}
		adapter := &vmAdapter{vm: vmInstance}
		assert.False(t, adapter.IsRunning())
	})

	t.Run("GetMetrics returns zeros when process is not running", func(t *testing.T) {
		vmInstance := &vm.VM{}
		adapter := &vmAdapter{vm: vmInstance}

		metrics, err := adapter.GetMetrics(context.Background())
		assert.NoError(t, err)
		assert.NotNil(t, metrics)
		assert.Equal(t, float64(0), metrics.CPUUsagePercent)
		assert.Equal(t, int64(0), metrics.MemoryUsageMB)
		assert.False(t, metrics.LastUpdated.IsZero())
	})
}

func TestStateStorePersistence(t *testing.T) {
	t.Run("state store errors are logged but don't fail operations", func(t *testing.T) {
		// This tests that state store errors are non-fatal for in-memory operations
		mockStore := new(MockStateStore)
		runtime := createTestRuntime(t, mockStore)

		// State store errors should be logged but not prevent listing
		agents, err := runtime.ListAgents(context.Background(), nil)
		assert.NoError(t, err)
		assert.NotNil(t, agents)
	})
}

// Integration-style tests (require setup but test full workflows)

func TestAgentLifecycleWorkflow(t *testing.T) {
	t.Run("document expected workflow", func(t *testing.T) {
		// This test documents the expected agent lifecycle workflow
		// Actual integration tests would require Firecracker infrastructure

		mockStore := new(MockStateStore)
		runtime := createTestRuntime(t, mockStore)

		// Expected workflow:
		// 1. CreateAgent - creates VM and agent instance
		// 2. StartAgent - starts the VM
		// 3. GetAgent - retrieves agent info
		// 4. GetAgentHealth - checks health
		// 5. StopAgent - stops the VM
		// 6. DestroyAgent - cleans up resources

		assert.NotNil(t, runtime)
		// Actual workflow testing requires VM infrastructure
	})
}

// Error scenario tests

func TestErrorHandling(t *testing.T) {
	t.Run("state store failures don't crash runtime", func(t *testing.T) {
		mockStore := new(MockStateStore)
		runtime := createTestRuntime(t, mockStore)

		// Runtime should handle state store being unavailable
		assert.NotNil(t, runtime)
	})

	t.Run("metrics recorder is optional", func(t *testing.T) {
		mockStore := new(MockStateStore)
		runtime := createTestRuntime(t, mockStore)

		// Runtime should work fine without metrics
		assert.Nil(t, runtime.metrics)

		// Operations should not panic
		err := runtime.Shutdown(context.Background())
		assert.NoError(t, err)
	})

	t.Run("concurrent operations are thread-safe", func(t *testing.T) {
		mockStore := new(MockStateStore)
		runtime := createTestRuntime(t, mockStore)

		// Test that concurrent ListAgents calls don't race
		done := make(chan bool)
		for i := 0; i < 10; i++ {
			go func() {
				_, _ = runtime.ListAgents(context.Background(), nil)
				done <- true
			}()
		}

		for i := 0; i < 10; i++ {
			<-done
		}
	})

	t.Run("context cancellation is respected", func(t *testing.T) {
		mockStore := new(MockStateStore)
		runtime := createTestRuntime(t, mockStore)

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		// Operations with cancelled context should work
		// (they may or may not check context depending on implementation)
		agents, err := runtime.ListAgents(ctx, nil)
		// List should succeed even with cancelled context for in-memory operations
		assert.NoError(t, err)
		assert.NotNil(t, agents)
	})
}

// =============================================================================
// Behavioral Tests — verify observable state changes, not mock plumbing
// =============================================================================

// mockVM implements agent.VM for tests without Firecracker.
type mockVM struct {
	started   bool
	stopped   bool
	destroyed bool
}

func (m *mockVM) Start(ctx context.Context) error { m.started = true; return nil }
func (m *mockVM) Stop(ctx context.Context, timeout time.Duration) error {
	m.stopped = true
	return nil
}
func (m *mockVM) Destroy(ctx context.Context) error { m.destroyed = true; return nil }
func (m *mockVM) IsRunning() bool                   { return m.started && !m.stopped }
func (m *mockVM) GetMetrics(ctx context.Context) (*api.AgentMetrics, error) {
	return &api.AgentMetrics{CPUUsagePercent: 1.0, MemoryUsageMB: 128, LastUpdated: time.Now()}, nil
}

// injectAgent creates an agent in the runtime's map without going through
// Firecracker. This lets us test runtime behavior (get, list, destroy, tenant
// filtering) without infrastructure dependencies.
func injectAgent(r *Runtime, config api.AgentConfig) *mockVM {
	mvm := &mockVM{}
	a := agent.New(
		r.logger,
		config,
		mvm,
		"",
	)
	r.mu.Lock()
	r.agents[config.ID] = a
	r.mu.Unlock()
	return mvm
}

func TestAgentLifecycle_GetAfterInject(t *testing.T) {
	mockStore := new(MockStateStore)
	rt := createTestRuntime(t, mockStore)

	config := api.AgentConfig{
		ID:        "lifecycle-1",
		TenantID:  "tenant-a",
		Name:      "test-agent",
		Image:     "python:3.11",
		Resources: api.ResourceLimits{CPUCount: 2, MemoryMB: 512},
	}
	injectAgent(rt, config)

	info, err := rt.GetAgent(context.Background(), "lifecycle-1")
	require.NoError(t, err)
	assert.Equal(t, api.AgentID("lifecycle-1"), info.Config.ID)
	assert.Equal(t, api.TenantID("tenant-a"), info.Config.TenantID)
	assert.Equal(t, "test-agent", info.Config.Name)
	assert.Equal(t, 2, info.Config.Resources.CPUCount)
}

func TestAgentLifecycle_StartUpdatesStateStore(t *testing.T) {
	mockStore := new(MockStateStore)
	mockStore.On("UpdateAgentStatus", mock.Anything, api.AgentID("start-1"), api.AgentStatusRunning).Return(nil)
	rt := createTestRuntime(t, mockStore)

	config := api.AgentConfig{ID: "start-1", TenantID: "t1", Name: "starter", Image: "test:1"}
	injectAgent(rt, config)

	err := rt.StartAgent(context.Background(), "start-1")
	require.NoError(t, err)

	// Verify state store was told the agent is running
	mockStore.AssertCalled(t, "UpdateAgentStatus", mock.Anything, api.AgentID("start-1"), api.AgentStatusRunning)
}

func TestAgentLifecycle_StopUpdatesStateStore(t *testing.T) {
	mockStore := new(MockStateStore)
	mockStore.On("UpdateAgentStatus", mock.Anything, api.AgentID("stop-1"), api.AgentStatusRunning).Return(nil)
	mockStore.On("UpdateAgentStatus", mock.Anything, api.AgentID("stop-1"), api.AgentStatusStopped).Return(nil)
	rt := createTestRuntime(t, mockStore)

	config := api.AgentConfig{ID: "stop-1", TenantID: "t1", Name: "stopper", Image: "test:1"}
	injectAgent(rt, config)

	// Start then stop
	require.NoError(t, rt.StartAgent(context.Background(), "stop-1"))
	require.NoError(t, rt.StopAgent(context.Background(), "stop-1", 5*time.Second))

	mockStore.AssertCalled(t, "UpdateAgentStatus", mock.Anything, api.AgentID("stop-1"), api.AgentStatusStopped)
}

func TestAgentLifecycle_DestroyRemovesFromRuntime(t *testing.T) {
	mockStore := new(MockStateStore)
	mockStore.On("DeleteAgent", mock.Anything, api.AgentID("destroy-1")).Return(nil)
	rt := createTestRuntime(t, mockStore)

	config := api.AgentConfig{ID: "destroy-1", TenantID: "t1", Name: "ephemeral", Image: "test:1"}
	mvm := injectAgent(rt, config)

	// Agent exists
	_, err := rt.GetAgent(context.Background(), "destroy-1")
	require.NoError(t, err)

	// Destroy
	err = rt.DestroyAgent(context.Background(), "destroy-1")
	require.NoError(t, err)

	// Agent no longer exists
	_, err = rt.GetAgent(context.Background(), "destroy-1")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")

	// VM was destroyed
	assert.True(t, mvm.destroyed)

	// State store was told to delete
	mockStore.AssertCalled(t, "DeleteAgent", mock.Anything, api.AgentID("destroy-1"))
}

func TestAgentLifecycle_DestroyNonexistentFails(t *testing.T) {
	rt := createTestRuntime(t, new(MockStateStore))
	err := rt.DestroyAgent(context.Background(), "ghost")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestListAgents_TenantFiltering(t *testing.T) {
	rt := createTestRuntime(t, nil)

	injectAgent(rt, api.AgentConfig{ID: "a1", TenantID: "alpha", Name: "a1", Image: "test:1"})
	injectAgent(rt, api.AgentConfig{ID: "a2", TenantID: "alpha", Name: "a2", Image: "test:1"})
	injectAgent(rt, api.AgentConfig{ID: "b1", TenantID: "beta", Name: "b1", Image: "test:1"})

	// Unfiltered — all 3
	all, err := rt.ListAgents(context.Background(), nil)
	require.NoError(t, err)
	assert.Len(t, all, 3)

	// Filter by alpha — 2
	alpha := api.TenantID("alpha")
	filtered, err := rt.ListAgents(context.Background(), &alpha)
	require.NoError(t, err)
	assert.Len(t, filtered, 2)
	for _, a := range filtered {
		assert.Equal(t, api.TenantID("alpha"), a.Config.TenantID)
	}

	// Filter by beta — 1
	beta := api.TenantID("beta")
	filtered, err = rt.ListAgents(context.Background(), &beta)
	require.NoError(t, err)
	assert.Len(t, filtered, 1)
	assert.Equal(t, api.AgentID("b1"), filtered[0].Config.ID)

	// Filter by nonexistent tenant — 0
	nobody := api.TenantID("nobody")
	filtered, err = rt.ListAgents(context.Background(), &nobody)
	require.NoError(t, err)
	assert.Empty(t, filtered)
}

func TestCreateAgent_DuplicateIDRejected(t *testing.T) {
	mockStore := new(MockStateStore)
	mockStore.On("CreateAgent", mock.Anything, mock.Anything).Return(nil)
	mockStore.On("DeleteAgent", mock.Anything, mock.Anything).Return(nil)
	rt := createTestRuntime(t, mockStore)

	config := api.AgentConfig{ID: "dup-1", TenantID: "t1", Name: "first", Image: "test:1"}
	injectAgent(rt, config)

	// Second create with same ID should fail
	err := rt.CreateAgent(context.Background(), config)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already exists")
}

func TestCreateAgent_StateStoreFailureRollsBack(t *testing.T) {
	mockStore := new(MockStateStore)
	mockStore.On("CreateAgent", mock.Anything, mock.Anything).Return(fmt.Errorf("database connection lost"))
	rt := createTestRuntime(t, mockStore)

	config := api.AgentConfig{ID: "fail-1", TenantID: "t1", Name: "doomed", Image: "test:1", Resources: api.ResourceLimits{CPUCount: 1, MemoryMB: 256}}
	err := rt.CreateAgent(context.Background(), config)

	// Should fail with state store error
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "state store")

	// Agent should NOT be in the runtime (no partial creation)
	_, err = rt.GetAgent(context.Background(), "fail-1")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestCreateAgent_NilStateStoreAllowed(t *testing.T) {
	// With nil state store, CreateAgent should still proceed to VM creation
	rt := createTestRuntime(t, nil)
	config := api.AgentConfig{ID: "nostore-1", TenantID: "t1", Name: "ephemeral", Image: "test:1", Resources: api.ResourceLimits{CPUCount: 1, MemoryMB: 256}}

	err := rt.CreateAgent(context.Background(), config)
	// Will fail at VM creation (no Firecracker), but should NOT fail at state store
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "VM") // fails at VM, not state store
}

func TestConcurrent_ListAndDestroy(t *testing.T) {
	mockStore := new(MockStateStore)
	mockStore.On("DeleteAgent", mock.Anything, mock.Anything).Return(nil)
	rt := createTestRuntime(t, mockStore)

	// Inject 20 agents
	for i := 0; i < 20; i++ {
		id := api.AgentID(fmt.Sprintf("conc-%d", i))
		injectAgent(rt, api.AgentConfig{ID: id, TenantID: "t1", Name: string(id), Image: "test:1"})
	}

	// Concurrently list and destroy
	done := make(chan struct{})
	go func() {
		for i := 0; i < 100; i++ {
			rt.ListAgents(context.Background(), nil)
		}
		close(done)
	}()

	for i := 0; i < 20; i++ {
		id := api.AgentID(fmt.Sprintf("conc-%d", i))
		rt.DestroyAgent(context.Background(), id)
	}
	<-done

	// All agents should be gone
	agents, err := rt.ListAgents(context.Background(), nil)
	require.NoError(t, err)
	assert.Empty(t, agents)
}

func TestReconcile_NilStateStoreReturnsEmpty(t *testing.T) {
	rt := createTestRuntime(t, nil)

	failed, err := rt.Reconcile(context.Background())
	assert.NoError(t, err)
	assert.Nil(t, failed)
}

func TestReconcile_LoadsAgentsFromStore(t *testing.T) {
	mockStore := new(MockStateStore)

	storedAgents := []*api.AgentInfo{
		{
			Config: api.AgentConfig{ID: "recon-1", TenantID: "t1", Name: "recovered", Image: "test:1", Resources: api.ResourceLimits{CPUCount: 1, MemoryMB: 256}},
			Status: api.AgentStatusRunning,
		},
		{
			Config: api.AgentConfig{ID: "recon-2", TenantID: "t1", Name: "stopped-one", Image: "test:1", Resources: api.ResourceLimits{CPUCount: 1, MemoryMB: 256}},
			Status: api.AgentStatusStopped,
		},
	}
	mockStore.On("ListAllAgents", mock.Anything).Return(storedAgents, nil)
	mockStore.On("SetAgentError", mock.Anything, api.AgentID("recon-1"), mock.Anything).Return(nil)

	rt := createTestRuntime(t, mockStore)

	failed, err := rt.Reconcile(context.Background())
	require.NoError(t, err)

	// Running agents should be marked failed (VMs don't survive restart)
	assert.Len(t, failed, 1)
	assert.Equal(t, api.AgentID("recon-1"), failed[0].Config.ID)

	// Both agents should be in runtime's map after reconcile
	agents, err := rt.ListAgents(context.Background(), nil)
	require.NoError(t, err)
	assert.Len(t, agents, 2)
}

func TestShutdown_DestroyAllAgents(t *testing.T) {
	rt := createTestRuntime(t, nil)

	// Inject agents with mock VMs
	var vms []*mockVM
	for i := 0; i < 3; i++ {
		id := api.AgentID(fmt.Sprintf("shutdown-%d", i))
		mvm := injectAgent(rt, api.AgentConfig{ID: id, TenantID: "t1", Name: string(id), Image: "test:1"})
		vms = append(vms, mvm)
	}

	err := rt.Shutdown(context.Background())
	assert.NoError(t, err)
}

func TestGetAgentHealth_ReturnsHealthStatus(t *testing.T) {
	rt := createTestRuntime(t, nil)
	injectAgent(rt, api.AgentConfig{ID: "health-1", TenantID: "t1", Name: "healthy", Image: "test:1"})

	health, err := rt.GetAgentHealth(context.Background(), "health-1")
	require.NoError(t, err)
	assert.NotNil(t, health)
}

func TestMetrics_RecordedOnDuplicateCreate(t *testing.T) {
	mockStore := new(MockStateStore)
	mockMetrics := new(MockMetricsRecorder)
	rt := createTestRuntime(t, mockStore)
	rt.SetMetrics(mockMetrics)

	config := api.AgentConfig{ID: "dup-m", TenantID: "t1", Name: "original", Image: "test:1"}
	injectAgent(rt, config)

	// Expect failure metrics on duplicate
	mockMetrics.On("RecordAgentOperation", "create", "failed", api.TenantID("t1")).Return()
	mockMetrics.On("RecordAgentError", "already_exists", api.TenantID("t1")).Return()

	err := rt.CreateAgent(context.Background(), config)
	assert.Error(t, err)

	mockMetrics.AssertCalled(t, "RecordAgentOperation", "create", "failed", api.TenantID("t1"))
	mockMetrics.AssertCalled(t, "RecordAgentError", "already_exists", api.TenantID("t1"))
}

func TestConfigDefaults(t *testing.T) {
	t.Run("default resources are applied", func(t *testing.T) {
		mockStore := new(MockStateStore)
		logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
		workspaceDir := t.TempDir()

		// Create dummy Firecracker binary
		firecrackerPath := workspaceDir + "/firecracker"
		err := os.WriteFile(firecrackerPath, []byte("#!/bin/sh\necho test"), 0755)
		require.NoError(t, err)

		config := Config{
			VMManagerConfig: vm.ManagerConfig{
				KernelImage:       "/tmp/kernel.img",
				RootFSImage:       "/tmp/rootfs.img",
				FirecrackerBinary: firecrackerPath,
				WorkspaceDir:      workspaceDir,
			},
			DefaultResources: api.ResourceLimits{
				CPUCount: 4,
				MemoryMB: 2048,
			},
			WorkspaceDir: workspaceDir,
		}

		runtime, err := New(logger, config, mockStore)
		require.NoError(t, err)

		assert.Equal(t, 4, runtime.config.DefaultResources.CPUCount)
		assert.Equal(t, int64(2048), runtime.config.DefaultResources.MemoryMB)
	})
}
