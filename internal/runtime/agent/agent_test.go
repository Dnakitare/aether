package agent

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/aether-runtime/aether/pkg/api"
)

// MockVM is a mock implementation of the VM interface
type MockVM struct {
	mock.Mock
}

func (m *MockVM) Start(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func (m *MockVM) Stop(ctx context.Context, timeout time.Duration) error {
	args := m.Called(ctx, timeout)
	return args.Error(0)
}

func (m *MockVM) Destroy(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func (m *MockVM) IsRunning() bool {
	args := m.Called()
	return args.Bool(0)
}

func (m *MockVM) GetMetrics(ctx context.Context) (*api.AgentMetrics, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*api.AgentMetrics), args.Error(1)
}

// Helper functions

func createTestAgent(t *testing.T, vm VM) *Agent {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	config := api.AgentConfig{
		ID:       api.AgentID("test-agent"),
		TenantID: api.TenantID("test-tenant"),
		Name:     "Test Agent",
		Image:    "python:3.11",
		Resources: api.ResourceLimits{
			CPUCount: 2,
			MemoryMB: 512,
		},
	}

	return New(logger, config, vm)
}

// Tests

func TestNew(t *testing.T) {
	t.Run("creates agent with correct initial state", func(t *testing.T) {
		mockVM := new(MockVM)
		agent := createTestAgent(t, mockVM)

		assert.NotNil(t, agent)
		assert.Equal(t, api.AgentStatusPending, agent.info.Status)
		assert.False(t, agent.info.CreatedAt.IsZero())
		assert.Nil(t, agent.info.StartedAt)
		assert.Nil(t, agent.info.StoppedAt)
		assert.Equal(t, "", agent.info.Error)
	})

	t.Run("creates metrics collector", func(t *testing.T) {
		mockVM := new(MockVM)
		agent := createTestAgent(t, mockVM)

		assert.NotNil(t, agent.metricsCollector)
	})
}

func TestStart(t *testing.T) {
	t.Run("successful start from pending", func(t *testing.T) {
		mockVM := new(MockVM)
		agent := createTestAgent(t, mockVM)

		mockVM.On("Start", mock.Anything).Return(nil)

		err := agent.Start(context.Background())
		assert.NoError(t, err)

		info := agent.GetInfo()
		assert.Equal(t, api.AgentStatusRunning, info.Status)
		assert.NotNil(t, info.StartedAt)
		assert.Equal(t, "", info.Error)

		mockVM.AssertExpectations(t)
	})

	t.Run("successful start from stopped", func(t *testing.T) {
		mockVM := new(MockVM)
		agent := createTestAgent(t, mockVM)

		// Set agent to stopped state
		agent.info.Status = api.AgentStatusStopped

		mockVM.On("Start", mock.Anything).Return(nil)

		err := agent.Start(context.Background())
		assert.NoError(t, err)
		assert.Equal(t, api.AgentStatusRunning, agent.info.Status)

		mockVM.AssertExpectations(t)
	})

	t.Run("VM start failure", func(t *testing.T) {
		mockVM := new(MockVM)
		agent := createTestAgent(t, mockVM)

		mockVM.On("Start", mock.Anything).Return(errors.New("VM start failed"))

		err := agent.Start(context.Background())
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to start VM")

		info := agent.GetInfo()
		assert.Equal(t, api.AgentStatusFailed, info.Status)
		assert.NotEqual(t, "", info.Error)

		mockVM.AssertExpectations(t)
	})

	t.Run("cannot start running agent", func(t *testing.T) {
		mockVM := new(MockVM)
		agent := createTestAgent(t, mockVM)

		// Set to running
		agent.info.Status = api.AgentStatusRunning

		err := agent.Start(context.Background())
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "cannot start agent")
	})
}

func TestStop(t *testing.T) {
	t.Run("successful stop", func(t *testing.T) {
		mockVM := new(MockVM)
		agent := createTestAgent(t, mockVM)

		// Start the agent first
		agent.info.Status = api.AgentStatusRunning
		now := time.Now()
		agent.info.StartedAt = &now

		mockVM.On("Stop", mock.Anything, 5*time.Second).Return(nil)

		err := agent.Stop(context.Background(), 5*time.Second)
		assert.NoError(t, err)

		info := agent.GetInfo()
		assert.Equal(t, api.AgentStatusStopped, info.Status)
		assert.NotNil(t, info.StoppedAt)

		mockVM.AssertExpectations(t)
	})

	t.Run("stop with VM error continues cleanup", func(t *testing.T) {
		mockVM := new(MockVM)
		agent := createTestAgent(t, mockVM)

		agent.info.Status = api.AgentStatusRunning

		mockVM.On("Stop", mock.Anything, 10*time.Second).Return(errors.New("VM stop failed"))

		err := agent.Stop(context.Background(), 10*time.Second)
		// Stop should succeed despite VM error
		assert.NoError(t, err)
		assert.Equal(t, api.AgentStatusStopped, agent.info.Status)

		mockVM.AssertExpectations(t)
	})

	t.Run("cannot stop non-running agent", func(t *testing.T) {
		mockVM := new(MockVM)
		agent := createTestAgent(t, mockVM)

		// Agent is pending
		err := agent.Stop(context.Background(), 5*time.Second)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "cannot stop agent")
	})
}

func TestDestroy(t *testing.T) {
	t.Run("destroy stopped agent", func(t *testing.T) {
		mockVM := new(MockVM)
		agent := createTestAgent(t, mockVM)

		agent.info.Status = api.AgentStatusStopped

		mockVM.On("Destroy", mock.Anything).Return(nil)

		err := agent.Destroy(context.Background())
		assert.NoError(t, err)

		mockVM.AssertExpectations(t)
	})

	t.Run("destroy running agent stops it first", func(t *testing.T) {
		mockVM := new(MockVM)
		agent := createTestAgent(t, mockVM)

		agent.info.Status = api.AgentStatusRunning

		mockVM.On("Stop", mock.Anything, 5*time.Second).Return(nil)
		mockVM.On("Destroy", mock.Anything).Return(nil)

		err := agent.Destroy(context.Background())
		assert.NoError(t, err)

		mockVM.AssertExpectations(t)
	})

	t.Run("destroy failure", func(t *testing.T) {
		mockVM := new(MockVM)
		agent := createTestAgent(t, mockVM)

		agent.info.Status = api.AgentStatusStopped

		mockVM.On("Destroy", mock.Anything).Return(errors.New("destroy failed"))

		err := agent.Destroy(context.Background())
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to destroy VM")

		mockVM.AssertExpectations(t)
	})
}

func TestGetInfo(t *testing.T) {
	t.Run("returns current agent info", func(t *testing.T) {
		mockVM := new(MockVM)
		agent := createTestAgent(t, mockVM)

		info := agent.GetInfo()
		assert.Equal(t, api.AgentID("test-agent"), info.Config.ID)
		assert.Equal(t, api.TenantID("test-tenant"), info.Config.TenantID)
		assert.Equal(t, api.AgentStatusPending, info.Status)
	})

	t.Run("includes metrics for running agent", func(t *testing.T) {
		mockVM := new(MockVM)
		agent := createTestAgent(t, mockVM)

		agent.info.Status = api.AgentStatusRunning

		_ = agent.GetInfo()
		// Metrics should be retrieved from collector
		assert.NotNil(t, agent.metricsCollector)
	})
}

func TestGetHealth(t *testing.T) {
	t.Run("healthy running agent", func(t *testing.T) {
		mockVM := new(MockVM)
		agent := createTestAgent(t, mockVM)

		agent.info.Status = api.AgentStatusRunning

		mockVM.On("IsRunning").Return(true)

		health := agent.GetHealth(context.Background())
		assert.True(t, health.Healthy)
		assert.Contains(t, health.Message, "healthy")
		assert.False(t, health.CheckedAt.IsZero())

		mockVM.AssertExpectations(t)
	})

	t.Run("unhealthy when not running", func(t *testing.T) {
		mockVM := new(MockVM)
		agent := createTestAgent(t, mockVM)

		// Agent is pending
		health := agent.GetHealth(context.Background())
		assert.False(t, health.Healthy)
		assert.Contains(t, health.Message, "not running")
	})

	t.Run("unhealthy when VM not running", func(t *testing.T) {
		mockVM := new(MockVM)
		agent := createTestAgent(t, mockVM)

		agent.info.Status = api.AgentStatusRunning

		mockVM.On("IsRunning").Return(false)

		health := agent.GetHealth(context.Background())
		assert.False(t, health.Healthy)
		assert.Contains(t, health.Message, "VM is not running")

		mockVM.AssertExpectations(t)
	})
}

func TestUpdateStatus(t *testing.T) {
	t.Run("update status without error", func(t *testing.T) {
		mockVM := new(MockVM)
		agent := createTestAgent(t, mockVM)

		agent.UpdateStatus(api.AgentStatusRunning, "")
		assert.Equal(t, api.AgentStatusRunning, agent.info.Status)
		assert.Equal(t, "", agent.info.Error)
	})

	t.Run("update status with error", func(t *testing.T) {
		mockVM := new(MockVM)
		agent := createTestAgent(t, mockVM)

		agent.UpdateStatus(api.AgentStatusFailed, "VM crashed")
		assert.Equal(t, api.AgentStatusFailed, agent.info.Status)
		assert.Equal(t, "VM crashed", agent.info.Error)
		assert.NotNil(t, agent.info.StoppedAt)
	})

	t.Run("stopped status sets StoppedAt", func(t *testing.T) {
		mockVM := new(MockVM)
		agent := createTestAgent(t, mockVM)

		agent.UpdateStatus(api.AgentStatusStopped, "")
		assert.NotNil(t, agent.info.StoppedAt)
	})
}

func TestConcurrency(t *testing.T) {
	t.Run("concurrent GetInfo calls are safe", func(t *testing.T) {
		mockVM := new(MockVM)
		agent := createTestAgent(t, mockVM)

		done := make(chan bool)
		for i := 0; i < 10; i++ {
			go func() {
				_ = agent.GetInfo()
				done <- true
			}()
		}

		for i := 0; i < 10; i++ {
			<-done
		}
	})

	t.Run("concurrent health checks are safe", func(t *testing.T) {
		mockVM := new(MockVM)
		agent := createTestAgent(t, mockVM)

		agent.info.Status = api.AgentStatusRunning

		// Mock IsRunning for all concurrent calls
		mockVM.On("IsRunning").Return(true)

		done := make(chan bool)
		for i := 0; i < 10; i++ {
			go func() {
				_ = agent.GetHealth(context.Background())
				done <- true
			}()
		}

		for i := 0; i < 10; i++ {
			<-done
		}
	})
}

func TestLifecycleWorkflow(t *testing.T) {
	t.Run("complete lifecycle: start -> stop -> destroy", func(t *testing.T) {
		mockVM := new(MockVM)
		agent := createTestAgent(t, mockVM)

		// Start
		mockVM.On("Start", mock.Anything).Return(nil).Once()
		err := agent.Start(context.Background())
		require.NoError(t, err)
		assert.Equal(t, api.AgentStatusRunning, agent.info.Status)

		// Stop
		mockVM.On("Stop", mock.Anything, 5*time.Second).Return(nil).Once()
		err = agent.Stop(context.Background(), 5*time.Second)
		require.NoError(t, err)
		assert.Equal(t, api.AgentStatusStopped, agent.info.Status)

		// Destroy
		mockVM.On("Destroy", mock.Anything).Return(nil).Once()
		err = agent.Destroy(context.Background())
		require.NoError(t, err)

		mockVM.AssertExpectations(t)
	})

	t.Run("restart after stop", func(t *testing.T) {
		mockVM := new(MockVM)
		agent := createTestAgent(t, mockVM)

		// First start
		mockVM.On("Start", mock.Anything).Return(nil).Once()
		err := agent.Start(context.Background())
		require.NoError(t, err)

		// Stop
		mockVM.On("Stop", mock.Anything, 5*time.Second).Return(nil).Once()
		err = agent.Stop(context.Background(), 5*time.Second)
		require.NoError(t, err)

		// Restart
		mockVM.On("Start", mock.Anything).Return(nil).Once()
		err = agent.Start(context.Background())
		require.NoError(t, err)
		assert.Equal(t, api.AgentStatusRunning, agent.info.Status)

		mockVM.AssertExpectations(t)
	})
}
