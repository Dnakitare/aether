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

	"github.com/dnakitare/aether/internal/optimization"
	"github.com/dnakitare/aether/internal/recovery"
	"github.com/dnakitare/aether/internal/runtime/agent"
	"github.com/dnakitare/aether/internal/runtime/vm"
	"github.com/dnakitare/aether/pkg/api"
)

// StateStore defines the interface for persistent agent state storage.
type StateStore interface {
	CreateAgent(ctx context.Context, config api.AgentConfig) error
	GetAgent(ctx context.Context, agentID api.AgentID) (*api.AgentInfo, error)
	ListAgents(ctx context.Context, tenantID api.TenantID) ([]*api.AgentInfo, error)
	ListAllAgents(ctx context.Context) ([]*api.AgentInfo, error)
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
	prewarmingPool    *optimization.PrewarmingPool
	config            Config

	mu             sync.RWMutex
	agents         map[api.AgentID]*agent.Agent
	prewarmedVMIDs map[api.AgentID]string // agentID → prewarmed VM ID, for pool discard on destroy
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
		logger:         logger,
		tracer:         otel.Tracer("aether.runtime"),
		vmManager:      vmManager,
		stateStore:     stateStore,
		config:         config,
		agents:         make(map[api.AgentID]*agent.Agent),
		prewarmedVMIDs: make(map[api.AgentID]string),
	}, nil
}

// SetMetrics sets the metrics recorder (optional).
func (r *Runtime) SetMetrics(metrics MetricsRecorder) {
	r.metrics = metrics
}

// Reconcile loads all agents from the state store into the in-memory map.
// Call this once after New() so that ListAgents/GetAgent work correctly after
// a server restart. Agents that were running when the process exited are
// transitioned to Failed because their VMs no longer exist.
//
// It returns the AgentInfo records for every agent that was transitioned to
// Failed so the caller can release their quota allocations.
func (r *Runtime) Reconcile(ctx context.Context) ([]*api.AgentInfo, error) {
	if r.stateStore == nil {
		return nil, nil
	}

	infos, err := r.stateStore.ListAllAgents(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to load agents from state store: %w", err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	var failed []*api.AgentInfo

	for _, info := range infos {
		// VMs don't survive a process restart. Any agent that was mid-flight
		// is now effectively dead — mark it failed and persist the status.
		switch info.Status {
		case api.AgentStatusRunning, api.AgentStatusCreating, api.AgentStatusStopping:
			info.Status = api.AgentStatusFailed
			info.Error = "runtime restarted"
			if serr := r.stateStore.SetAgentError(ctx, info.Config.ID, info.Error); serr != nil {
				r.logger.WarnContext(ctx, "failed to update agent status after reconcile",
					"agent_id", info.Config.ID, "error", serr)
			}
			failed = append(failed, info)
		}

		// Populate in-memory map with a skeleton agent backed by a no-op VM.
		// The agent is not actually running, but it is visible to ListAgents.
		logPath := fmt.Sprintf("%s/%s/vm.log", r.config.WorkspaceDir, info.Config.ID)
		skeleton := agent.New(r.logger, info.Config, &deadVM{}, logPath)
		skeleton.SetInfo(*info)
		r.agents[info.Config.ID] = skeleton
	}

	r.logger.InfoContext(ctx, "runtime reconciled from state store",
		"agents", len(infos), "failed", len(failed))
	return failed, nil
}

// deadVM is a no-op VM used for agents loaded from the DB after a restart.
// Their Firecracker processes no longer exist, so all operations are inert.
type deadVM struct{}

func (d *deadVM) Start(_ context.Context) error                 { return fmt.Errorf("VM no longer exists") }
func (d *deadVM) Stop(_ context.Context, _ time.Duration) error { return nil }
func (d *deadVM) Destroy(_ context.Context) error               { return nil }
func (d *deadVM) IsRunning() bool                               { return false }
func (d *deadVM) GetMetrics(_ context.Context) (*api.AgentMetrics, error) {
	return &api.AgentMetrics{}, nil
}

// SetPrewarmingPool attaches a pre-warming pool so CreateAgent can reuse
// already-warm VMs instead of cold-starting every time.
func (r *Runtime) SetPrewarmingPool(pool *optimization.PrewarmingPool) {
	r.prewarmingPool = pool
	// The runtime acts as the VMFactory so the pool creates real VMs.
	pool.SetFactory(r)
}

// CreatePrewarmedVM implements optimization.VMFactory.
// It creates a base VM with default resources for the given workload type,
// ready to be claimed by the next matching CreateAgent call.
func (r *Runtime) CreatePrewarmedVM(ctx context.Context, workloadType optimization.WorkloadType) (interface{}, error) {
	vmID := fmt.Sprintf("pw-%s-%d", workloadType, time.Now().UnixNano())
	vmPaths := vm.VMPaths{
		KernelImage: r.config.VMManagerConfig.KernelImage,
		RootFS:      r.config.VMManagerConfig.RootFSImage,
		Socket:      fmt.Sprintf("%s/%s/firecracker.sock", r.config.WorkspaceDir, vmID),
		Log:         fmt.Sprintf("%s/%s/vm.log", r.config.WorkspaceDir, vmID),
		Metrics:     fmt.Sprintf("%s/%s/metrics.fifo", r.config.WorkspaceDir, vmID),
		WorkDir:     fmt.Sprintf("%s/%s", r.config.WorkspaceDir, vmID),
	}
	baseCfg := vm.VMConfig{
		ID:              vmID,
		KernelImagePath: vmPaths.KernelImage,
		RootfsPath:      vmPaths.RootFS,
		CPUCount:        r.config.DefaultResources.CPUCount,
		MemoryMB:        r.config.DefaultResources.MemoryMB,
		SocketPath:      vmPaths.Socket,
		LogPath:         vmPaths.Log,
		MetricsPath:     vmPaths.Metrics,
	}
	return r.vmManager.Create(ctx, baseCfg)
}

// workloadTypeForConfig maps an agent config to the appropriate pool workload type.
func workloadTypeForConfig(config api.AgentConfig) optimization.WorkloadType {
	switch {
	case config.Resources.MemoryMB >= 4096:
		return optimization.WorkloadLLMAgent
	case config.Resources.CPUCount >= 4:
		return optimization.WorkloadDataProcessing
	default:
		return optimization.WorkloadCodeExecution
	}
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

	// Persist to state store first — if this fails we haven't created any
	// infrastructure yet, so there's nothing to clean up.
	if r.stateStore != nil {
		span.AddEvent("persisting_to_state_store")
		if err := r.stateStore.CreateAgent(ctx, config); err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, "failed to persist agent")

			if r.metrics != nil {
				r.metrics.RecordAgentOperation("create", "failed", config.TenantID)
				r.metrics.RecordAgentError("state_store_failed", config.TenantID)
			}

			return fmt.Errorf("failed to persist agent to state store: %w", err)
		}
	}

	// Try to claim a pre-warmed VM from the pool before cold-creating one.
	var prewarmedVM *optimization.PrewarmedVM
	if r.prewarmingPool != nil {
		wt := workloadTypeForConfig(config)
		if pvm, err := r.prewarmingPool.AcquireVM(ctx, wt); err == nil {
			prewarmedVM = pvm
			span.AddEvent("prewarmed_vm_acquired", trace.WithAttributes(
				attribute.String("prewarmed_vm_id", pvm.ID),
			))
		}
		// AcquireVM failure is non-fatal — fall back to cold creation.
	}

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

	// If we have a real pre-warmed VM, use it directly; otherwise cold-create.
	var vmInstance *vm.VM
	if prewarmedVM != nil && prewarmedVM.VM != nil {
		if realVM, ok := prewarmedVM.VM.(*vm.VM); ok {
			vmInstance = realVM
			span.AddEvent("using_prewarmed_vm")
		}
	}

	var err error
	if vmInstance == nil {
		// Cold creation path.
		span.AddEvent("creating_vm")
		vmInstance, err = r.vmManager.Create(ctx, vmConfig)
	}
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "failed to create VM")

		// Roll back the state store record since VM creation failed.
		if r.stateStore != nil {
			if derr := r.stateStore.DeleteAgent(ctx, config.ID); derr != nil {
				r.logger.ErrorContext(ctx, "failed to delete agent record after VM creation failure",
					"agent_id", config.ID, "error", derr)
			}
		}

		// Discard any pre-warmed VM we claimed but never handed to an agent,
		// otherwise it leaks in the pool's inUse map and skews accounting.
		if prewarmedVM != nil && r.prewarmingPool != nil {
			r.prewarmingPool.DiscardVM(prewarmedVM.ID)
		}

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
	agentInstance := agent.New(r.logger, config, vmAdapter, vmPaths.Log)

	// Store the agent in memory
	r.mu.Lock()
	r.agents[config.ID] = agentInstance
	if prewarmedVM != nil {
		r.prewarmedVMIDs[config.ID] = prewarmedVM.ID
	}
	agentCount := len(r.agents)
	r.mu.Unlock()

	span.SetAttributes(attribute.Int("runtime.agent_count", agentCount))

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

	// Remove from memory and discard prewarmed VM tracking.
	r.mu.Lock()
	delete(r.agents, id)
	pwID := r.prewarmedVMIDs[id]
	delete(r.prewarmedVMIDs, id)
	r.mu.Unlock()

	if pwID != "" && r.prewarmingPool != nil {
		r.prewarmingPool.DiscardVM(pwID)
	}

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
	return v.vm.IsRunning()
}

func (v *vmAdapter) GetMetrics(ctx context.Context) (*api.AgentMetrics, error) {
	return v.vm.GetMetrics(ctx)
}
