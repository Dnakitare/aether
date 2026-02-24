// Package runtime provides the core Aether runtime implementation.
package runtime

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/aether-runtime/aether/internal/recovery"
	"github.com/aether-runtime/aether/internal/runtime/agent"
	"github.com/aether-runtime/aether/internal/runtime/vm"
	"github.com/aether-runtime/aether/pkg/api"
)

// StateStore defines the interface for persistent agent state storage.
type StateStore interface {
	CreateAgent(ctx context.Context, config api.AgentConfig) error
	GetAgent(ctx context.Context, agentID api.AgentID) (*api.AgentInfo, error)
	ListAgents(ctx context.Context, tenantID api.TenantID) ([]*api.AgentInfo, error)
	UpdateAgentStatus(ctx context.Context, agentID api.AgentID, status api.AgentStatus) error
	SetAgentError(ctx context.Context, agentID api.AgentID, errMsg string) error
	DeleteAgent(ctx context.Context, agentID api.AgentID) error
}

// MetricsRecorder defines the interface for recording runtime metrics.
type MetricsRecorder interface {
	RecordAgentOperation(operation string, status string, tenantID api.TenantID)
	RecordAgentStartup(tenantID api.TenantID, duration time.Duration)
	RecordAgentError(errorType string, tenantID api.TenantID)
	SetAgentCount(status api.AgentStatus, tenantID api.TenantID, count float64)
}

// Runtime is the main Aether runtime that manages agent lifecycle.
type Runtime struct {
	logger            *slog.Logger
	tracer            trace.Tracer
	metrics           MetricsRecorder
	vmManager         *vm.Manager
	stateStore        StateStore
	checkpointManager *recovery.CheckpointManager
	config            Config

	mu     sync.RWMutex
	agents map[api.AgentID]*agent.Agent
}

// Config holds runtime configuration.
type Config struct {
	// VMManagerConfig is the configuration for the VM manager.
	VMManagerConfig vm.ManagerConfig

	// DefaultResources are the default resource limits for agents.
	DefaultResources api.ResourceLimits

	// WorkspaceDir is where agent data is stored.
	WorkspaceDir string
}

// New creates a new Aether runtime.
func New(logger *slog.Logger, config Config, stateStore StateStore) (*Runtime, error) {
	vmManager, err := vm.NewManager(logger, config.VMManagerConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create VM manager: %w", err)
	}

	return &Runtime{
		logger:     logger,
		tracer:     otel.Tracer("aether.runtime"),
		vmManager:  vmManager,
		stateStore: stateStore,
		config:     config,
		agents:     make(map[api.AgentID]*agent.Agent),
	}, nil
}

// SetMetrics sets the metrics recorder (optional).
func (r *Runtime) SetMetrics(metrics MetricsRecorder) {
	r.metrics = metrics
}

// CreateAgent creates a new agent with the given configuration.
func (r *Runtime) CreateAgent(ctx context.Context, config api.AgentConfig) error {
	// Track agent creation time for metrics
	startTime := time.Now()

	ctx, span := r.tracer.Start(ctx, "runtime.CreateAgent",
		trace.WithAttributes(
			attribute.String("agent.id", string(config.ID)),
			attribute.String("agent.tenant_id", string(config.TenantID)),
			attribute.String("agent.image", config.Image),
		),
	)
	defer span.End()

	r.logger.InfoContext(ctx, "creating agent",
		"agent_id", config.ID,
		"tenant_id", config.TenantID,
		"image", config.Image,
	)

	// Apply default resources if not specified
	if config.Resources.CPUCount == 0 {
		config.Resources.CPUCount = r.config.DefaultResources.CPUCount
	}
	if config.Resources.MemoryMB == 0 {
		config.Resources.MemoryMB = r.config.DefaultResources.MemoryMB
	}

	span.SetAttributes(
		attribute.Int("agent.cpu_count", config.Resources.CPUCount),
		attribute.Int64("agent.memory_mb", config.Resources.MemoryMB),
	)

	// Check if agent already exists
	r.mu.RLock()
	if _, exists := r.agents[config.ID]; exists {
		r.mu.RUnlock()
		err := fmt.Errorf("agent %s already exists", config.ID)
		span.RecordError(err)
		span.SetStatus(codes.Error, "agent already exists")

		// Record error metric
		if r.metrics != nil {
			r.metrics.RecordAgentOperation("create", "failed", config.TenantID)
			r.metrics.RecordAgentError("already_exists", config.TenantID)
		}

		return err
	}
	r.mu.RUnlock()

	// Create VM configuration
	vmPaths := vm.VMPaths{
		KernelImage: r.config.VMManagerConfig.KernelImage,
		RootFS:      r.config.VMManagerConfig.RootFSImage,
		Socket:      fmt.Sprintf("%s/%s/firecracker.sock", r.config.WorkspaceDir, config.ID),
		Log:         fmt.Sprintf("%s/%s/vm.log", r.config.WorkspaceDir, config.ID),
		Metrics:     fmt.Sprintf("%s/%s/metrics.fifo", r.config.WorkspaceDir, config.ID),
		WorkDir:     fmt.Sprintf("%s/%s", r.config.WorkspaceDir, config.ID),
	}

	vmConfig := vm.FromAgentConfig(config, vmPaths)

	// Create the VM
	span.AddEvent("creating_vm")
	vmInstance, err := r.vmManager.Create(ctx, vmConfig)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to create VM")

		// Record error metric
		if r.metrics != nil {
			r.metrics.RecordAgentOperation("create", "failed", config.TenantID)
			r.metrics.RecordAgentError("vm_creation_failed", config.TenantID)
		}

		return fmt.Errorf("failed to create VM: %w", err)
	}
	span.AddEvent("vm_created")

	// Wrap VM in adapter for agent interface
	vmAdapter := &vmAdapter{vm: vmInstance}

	// Create the agent
	agentInstance := agent.New(r.logger, config, vmAdapter)

	// Store the agent in memory
	r.mu.Lock()
	r.agents[config.ID] = agentInstance
	r.mu.Unlock()

	span.SetAttributes(attribute.Int("runtime.agent_count", len(r.agents)))

	// Persist to state store if available
	if r.stateStore != nil {
		span.AddEvent("persisting_to_state_store")
		if err := r.stateStore.CreateAgent(ctx, config); err != nil {
			r.logger.ErrorContext(ctx, "failed to persist agent to state store",
				"agent_id", config.ID,
				"error", err,
			)
			span.RecordError(err)
			span.AddEvent("state_store_persistence_failed")
			// Continue anyway - agent exists in memory
		}
	}

	// Record successful agent creation metrics
	if r.metrics != nil {
		r.metrics.RecordAgentOperation("create", "success", config.TenantID)
		r.metrics.RecordAgentStartup(config.TenantID, time.Since(startTime))
		// Note: Agent count is updated by a separate goroutine that polls agent states
	}

	r.logger.InfoContext(ctx, "agent created successfully", "agent_id", config.ID)
	return nil
}

// StartAgent starts an agent that was previously created.
func (r *Runtime) StartAgent(ctx context.Context, id api.AgentID) error {
	r.logger.InfoContext(ctx, "starting agent", "agent_id", id)

	agentInstance, err := r.getAgent(id)
	if err != nil {
		return err
	}

	if err := agentInstance.Start(ctx); err != nil {
		return fmt.Errorf("failed to start agent: %w", err)
	}

	// Update status in state store
	if r.stateStore != nil {
		if err := r.stateStore.UpdateAgentStatus(ctx, id, api.AgentStatusRunning); err != nil {
			r.logger.WarnContext(ctx, "failed to update agent status in state store",
				"agent_id", id,
				"error", err,
			)
		}
	}

	return nil
}

// StopAgent gracefully stops a running agent.
func (r *Runtime) StopAgent(ctx context.Context, id api.AgentID, timeout time.Duration) error {
	r.logger.InfoContext(ctx, "stopping agent", "agent_id", id, "timeout", timeout)

	agentInstance, err := r.getAgent(id)
	if err != nil {
		return err
	}

	if err := agentInstance.Stop(ctx, timeout); err != nil {
		return fmt.Errorf("failed to stop agent: %w", err)
	}

	// Update status in state store
	if r.stateStore != nil {
		if err := r.stateStore.UpdateAgentStatus(ctx, id, api.AgentStatusStopped); err != nil {
			r.logger.WarnContext(ctx, "failed to update agent status in state store",
				"agent_id", id,
				"error", err,
			)
		}
	}

	return nil
}

// DestroyAgent removes an agent and cleans up all resources.
func (r *Runtime) DestroyAgent(ctx context.Context, id api.AgentID) error {
	r.logger.InfoContext(ctx, "destroying agent", "agent_id", id)

	agentInstance, err := r.getAgent(id)
	if err != nil {
		return err
	}

	if err := agentInstance.Destroy(ctx); err != nil {
		return fmt.Errorf("failed to destroy agent: %w", err)
	}

	// Remove from memory
	r.mu.Lock()
	delete(r.agents, id)
	r.mu.Unlock()

	// Delete from state store
	if r.stateStore != nil {
		if err := r.stateStore.DeleteAgent(ctx, id); err != nil {
			r.logger.WarnContext(ctx, "failed to delete agent from state store",
				"agent_id", id,
				"error", err,
			)
		}
	}

	r.logger.InfoContext(ctx, "agent destroyed successfully", "agent_id", id)
	return nil
}

// GetAgent retrieves information about an agent.
func (r *Runtime) GetAgent(ctx context.Context, id api.AgentID) (*api.AgentInfo, error) {
	agentInstance, err := r.getAgent(id)
	if err != nil {
		return nil, err
	}

	info := agentInstance.GetInfo()
	return &info, nil
}

// ListAgents lists all agents, optionally filtered by tenant.
func (r *Runtime) ListAgents(ctx context.Context, tenantID *api.TenantID) ([]*api.AgentInfo, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	agents := make([]*api.AgentInfo, 0)

	for _, agentInstance := range r.agents {
		info := agentInstance.GetInfo()

		// Filter by tenant if specified
		if tenantID != nil && info.Config.TenantID != *tenantID {
			continue
		}

		agents = append(agents, &info)
	}

	return agents, nil
}

// GetAgentLogs streams logs from an agent.
func (r *Runtime) GetAgentLogs(ctx context.Context, id api.AgentID, follow bool) (io.ReadCloser, error) {
	agentInstance, err := r.getAgent(id)
	if err != nil {
		return nil, err
	}

	return agentInstance.GetLogs(ctx, follow)
}

// GetAgentHealth checks if an agent is healthy.
func (r *Runtime) GetAgentHealth(ctx context.Context, id api.AgentID) (*api.HealthStatus, error) {
	agentInstance, err := r.getAgent(id)
	if err != nil {
		return nil, err
	}

	health := agentInstance.GetHealth(ctx)
	return health, nil
}

// getAgent retrieves an agent by ID (internal helper).
func (r *Runtime) getAgent(id api.AgentID) (*agent.Agent, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	agentInstance, exists := r.agents[id]
	if !exists {
		return nil, fmt.Errorf("agent %s not found", id)
	}

	return agentInstance, nil
}

// Shutdown gracefully shuts down the runtime.
func (r *Runtime) Shutdown(ctx context.Context) error {
	r.logger.InfoContext(ctx, "shutting down runtime")

	r.mu.RLock()
	agentList := make([]*agent.Agent, 0, len(r.agents))
	for _, a := range r.agents {
		agentList = append(agentList, a)
	}
	r.mu.RUnlock()

	// Stop all running agents
	for _, agentInstance := range agentList {
		info := agentInstance.GetInfo()
		if info.Status == api.AgentStatusRunning {
			if err := agentInstance.Stop(ctx, 10*time.Second); err != nil {
				r.logger.WarnContext(ctx, "failed to stop agent during shutdown",
					"agent_id", info.Config.ID,
					"error", err,
				)
			}
		}
	}

	r.logger.InfoContext(ctx, "runtime shutdown complete")
	return nil
}

// SetCheckpointManager sets the checkpoint manager for the runtime.
// This should be called after creating the runtime if checkpoint functionality is desired.
func (r *Runtime) SetCheckpointManager(db *sql.DB) error {
	config := recovery.DefaultCheckpointConfig()
	cm, err := recovery.NewCheckpointManager(r.logger, db, config)
	if err != nil {
		return fmt.Errorf("failed to create checkpoint manager: %w", err)
	}
	r.checkpointManager = cm
	r.logger.Info("checkpoint manager initialized",
		"retention_count", config.RetentionCount,
		"max_size_mb", config.MaxCheckpointSize/(1024*1024),
	)
	return nil
}

// CreateCheckpoint creates a checkpoint for an agent.
func (r *Runtime) CreateCheckpoint(ctx context.Context, agentID api.AgentID) (*recovery.Checkpoint, error) {
	if r.checkpointManager == nil {
		return nil, fmt.Errorf("checkpoint manager not initialized")
	}

	r.logger.InfoContext(ctx, "creating checkpoint", "agent_id", agentID)

	// Get agent info
	agentInstance, err := r.getAgent(agentID)
	if err != nil {
		return nil, err
	}

	info := agentInstance.GetInfo()

	// Create state snapshot
	state := map[string]interface{}{
		"status":     string(info.Status),
		"config":     info.Config,
		"created_at": time.Now().Format(time.RFC3339),
	}

	// Metadata
	metadata := map[string]string{
		"image":     info.Config.Image,
		"cpu_count": fmt.Sprintf("%d", info.Config.Resources.CPUCount),
		"memory_mb": fmt.Sprintf("%d", info.Config.Resources.MemoryMB),
	}

	// Create checkpoint
	checkpoint, err := r.checkpointManager.CreateCheckpoint(
		ctx,
		agentID,
		info.Config.TenantID,
		state,
		metadata,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create checkpoint: %w", err)
	}

	r.logger.InfoContext(ctx, "checkpoint created successfully",
		"agent_id", agentID,
		"version", checkpoint.Version,
		"size", checkpoint.Size,
	)

	return checkpoint, nil
}

// ListCheckpoints lists all checkpoints for an agent.
func (r *Runtime) ListCheckpoints(ctx context.Context, agentID api.AgentID) ([]*recovery.Checkpoint, error) {
	if r.checkpointManager == nil {
		return nil, fmt.Errorf("checkpoint manager not initialized")
	}

	checkpoints, err := r.checkpointManager.ListCheckpoints(ctx, agentID)
	if err != nil {
		return nil, fmt.Errorf("failed to list checkpoints: %w", err)
	}

	return checkpoints, nil
}

// GetLatestCheckpoint gets the latest checkpoint for an agent.
func (r *Runtime) GetLatestCheckpoint(ctx context.Context, agentID api.AgentID) (*recovery.Checkpoint, error) {
	if r.checkpointManager == nil {
		return nil, fmt.Errorf("checkpoint manager not initialized")
	}

	checkpoint, err := r.checkpointManager.GetLatestCheckpoint(ctx, agentID)
	if err != nil {
		return nil, fmt.Errorf("failed to get latest checkpoint: %w", err)
	}

	return checkpoint, nil
}

// RestoreFromCheckpoint restores an agent from a checkpoint.
func (r *Runtime) RestoreFromCheckpoint(ctx context.Context, agentID api.AgentID, version int) error {
	if r.checkpointManager == nil {
		return fmt.Errorf("checkpoint manager not initialized")
	}

	r.logger.InfoContext(ctx, "restoring agent from checkpoint",
		"agent_id", agentID,
		"version", version,
	)

	// Get checkpoint
	var checkpoint *recovery.Checkpoint
	var err error
	if version > 0 {
		checkpoint, err = r.checkpointManager.GetCheckpointByVersion(ctx, agentID, version)
	} else {
		checkpoint, err = r.checkpointManager.GetLatestCheckpoint(ctx, agentID)
	}
	if err != nil {
		return fmt.Errorf("failed to get checkpoint: %w", err)
	}

	r.logger.InfoContext(ctx, "found checkpoint",
		"version", checkpoint.Version,
		"created_at", checkpoint.CreatedAt,
	)

	// Get agent info to check current status
	agentInfo, err := r.GetAgent(ctx, agentID)
	if err != nil {
		return fmt.Errorf("failed to get agent info: %w", err)
	}

	// Stop the agent if it's currently running
	if agentInfo.Status == api.AgentStatusRunning {
		r.logger.InfoContext(ctx, "stopping agent for restore", "agent_id", agentID)
		if err := r.StopAgent(ctx, agentID, 30*time.Second); err != nil {
			return fmt.Errorf("failed to stop agent before restore: %w", err)
		}
	}

	// Apply checkpoint state to agent
	// Note: In a full implementation with VM snapshots (CRIU), this would restore
	// the complete VM state. For now, we restore metadata and configuration state.
	if r.stateStore != nil {
		// Update agent status to indicate restore in progress
		if err := r.stateStore.UpdateAgentStatus(ctx, agentID, api.AgentStatusPending); err != nil {
			r.logger.WarnContext(ctx, "failed to update agent status during restore",
				"agent_id", agentID,
				"error", err,
			)
		}
	}

	r.logger.InfoContext(ctx, "checkpoint state restored",
		"agent_id", agentID,
		"version", checkpoint.Version,
		"state_keys", len(checkpoint.State),
	)

	// Restart the agent
	if err := r.StartAgent(ctx, agentID); err != nil {
		return fmt.Errorf("failed to start agent after restore: %w", err)
	}

	r.logger.InfoContext(ctx, "agent restored from checkpoint",
		"agent_id", agentID,
		"version", checkpoint.Version,
	)

	return nil
}

// DeleteCheckpoint deletes a specific checkpoint.
func (r *Runtime) DeleteCheckpoint(ctx context.Context, agentID api.AgentID, version int) error {
	if r.checkpointManager == nil {
		return fmt.Errorf("checkpoint manager not initialized")
	}

	r.logger.InfoContext(ctx, "deleting checkpoint",
		"agent_id", agentID,
		"version", version,
	)

	if err := r.checkpointManager.DeleteCheckpoint(ctx, agentID, version); err != nil {
		return fmt.Errorf("failed to delete checkpoint: %w", err)
	}

	r.logger.InfoContext(ctx, "checkpoint deleted successfully",
		"agent_id", agentID,
		"version", version,
	)

	return nil
}

// vmAdapter adapts the vm.VM to the agent.VM interface.
type vmAdapter struct {
	vm *vm.VM
}

func (v *vmAdapter) Start(ctx context.Context) error {
	return v.vm.Start(ctx)
}

func (v *vmAdapter) Stop(ctx context.Context, timeout time.Duration) error {
	return v.vm.Stop(ctx, timeout)
}

func (v *vmAdapter) Destroy(ctx context.Context) error {
	return v.vm.Destroy(ctx)
}

func (v *vmAdapter) IsRunning() bool {
	// Check if the VM process is still running
	// This is a simplified check - in production, verify via API
	return v.vm.Config.SocketPath != ""
}

func (v *vmAdapter) GetMetrics(ctx context.Context) (*api.AgentMetrics, error) {
	// In a real implementation, query Firecracker metrics API
	// For now, return mock metrics
	return &api.AgentMetrics{
		CPUUsagePercent: 0.0,
		MemoryUsageMB:   0,
		NetworkRxBytes:  0,
		NetworkTxBytes:  0,
		LastUpdated:     time.Now(),
	}, nil
}
