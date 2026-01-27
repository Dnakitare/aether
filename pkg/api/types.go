// Package api provides core types and interfaces for the Aether runtime.
package api

import (
	"context"
	"io"
	"time"
)

// AgentID is a unique identifier for an agent.
type AgentID string

// TenantID is a unique identifier for a tenant.
type TenantID string

// AgentStatus represents the current state of an agent.
type AgentStatus string

const (
	// AgentStatusPending indicates the agent is queued for scheduling.
	AgentStatusPending AgentStatus = "pending"

	// AgentStatusCreating indicates the agent's VM is being created.
	AgentStatusCreating AgentStatus = "creating"

	// AgentStatusRunning indicates the agent is active and running.
	AgentStatusRunning AgentStatus = "running"

	// AgentStatusStopping indicates the agent is shutting down.
	AgentStatusStopping AgentStatus = "stopping"

	// AgentStatusStopped indicates the agent has been stopped.
	AgentStatusStopped AgentStatus = "stopped"

	// AgentStatusFailed indicates the agent encountered an error.
	AgentStatusFailed AgentStatus = "failed"
)

// ResourceLimits defines resource constraints for an agent.
type ResourceLimits struct {
	// CPUCount is the number of vCPUs allocated to the agent.
	CPUCount int `json:"cpu_count"`

	// MemoryMB is the amount of memory in megabytes.
	MemoryMB int64 `json:"memory_mb"`

	// DiskMB is the disk size in megabytes.
	DiskMB int64 `json:"disk_mb,omitempty"`

	// NetworkBandwidthMbps is the network bandwidth limit in Mbps.
	NetworkBandwidthMbps int `json:"network_bandwidth_mbps,omitempty"`
}

// AgentConfig defines the configuration for creating an agent.
type AgentConfig struct {
	// ID is the unique identifier for this agent.
	ID AgentID `json:"id"`

	// TenantID is the tenant this agent belongs to.
	TenantID TenantID `json:"tenant_id"`

	// Name is a human-readable name for the agent.
	Name string `json:"name"`

	// Image is the container image to run.
	Image string `json:"image"`

	// Resources defines resource limits for this agent.
	Resources ResourceLimits `json:"resources"`

	// Environment variables to inject into the agent.
	Env map[string]string `json:"env,omitempty"`

	// Secrets to inject into the agent (keys only, values fetched from vault).
	Secrets []string `json:"secrets,omitempty"`

	// Labels for metadata and scheduling hints.
	Labels map[string]string `json:"labels,omitempty"`
}

// AgentInfo contains runtime information about an agent.
type AgentInfo struct {
	// Config is the agent's configuration.
	Config AgentConfig `json:"config"`

	// Status is the current status of the agent.
	Status AgentStatus `json:"status"`

	// CreatedAt is when the agent was created.
	CreatedAt time.Time `json:"created_at"`

	// StartedAt is when the agent started running.
	StartedAt *time.Time `json:"started_at,omitempty"`

	// StoppedAt is when the agent stopped.
	StoppedAt *time.Time `json:"stopped_at,omitempty"`

	// Error message if the agent failed.
	Error string `json:"error,omitempty"`

	// Metrics contains current resource usage.
	Metrics *AgentMetrics `json:"metrics,omitempty"`
}

// AgentMetrics contains real-time metrics for an agent.
type AgentMetrics struct {
	// CPUUsagePercent is the current CPU usage (0-100 per vCPU).
	CPUUsagePercent float64 `json:"cpu_usage_percent"`

	// MemoryUsageMB is the current memory usage in MB.
	MemoryUsageMB int64 `json:"memory_usage_mb"`

	// NetworkRxBytes is total bytes received.
	NetworkRxBytes int64 `json:"network_rx_bytes"`

	// NetworkTxBytes is total bytes transmitted.
	NetworkTxBytes int64 `json:"network_tx_bytes"`

	// LastUpdated is when these metrics were collected.
	LastUpdated time.Time `json:"last_updated"`
}

// HealthStatus represents an agent's health check result.
type HealthStatus struct {
	// Healthy indicates if the agent is healthy.
	Healthy bool `json:"healthy"`

	// Message provides additional context.
	Message string `json:"message,omitempty"`

	// CheckedAt is when the health check was performed.
	CheckedAt time.Time `json:"checked_at"`
}

// Runtime defines the interface for the agent runtime.
type Runtime interface {
	// CreateAgent creates a new agent with the given configuration.
	CreateAgent(ctx context.Context, config AgentConfig) error

	// StartAgent starts an agent that was previously created.
	StartAgent(ctx context.Context, id AgentID) error

	// StopAgent gracefully stops a running agent.
	StopAgent(ctx context.Context, id AgentID, timeout time.Duration) error

	// DestroyAgent removes an agent and cleans up all resources.
	DestroyAgent(ctx context.Context, id AgentID) error

	// GetAgent retrieves information about an agent.
	GetAgent(ctx context.Context, id AgentID) (*AgentInfo, error)

	// ListAgents lists all agents, optionally filtered by tenant.
	ListAgents(ctx context.Context, tenantID *TenantID) ([]*AgentInfo, error)

	// GetAgentLogs streams logs from an agent.
	GetAgentLogs(ctx context.Context, id AgentID, follow bool) (io.ReadCloser, error)

	// GetAgentHealth checks if an agent is healthy.
	GetAgentHealth(ctx context.Context, id AgentID) (*HealthStatus, error)

	// Shutdown gracefully shuts down the runtime.
	Shutdown(ctx context.Context) error
}
