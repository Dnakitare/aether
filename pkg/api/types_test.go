package api_test

import (
	"testing"

	"github.com/aether-runtime/aether/pkg/api"
)

func TestAgentStatus(t *testing.T) {
	tests := []struct {
		name   string
		status api.AgentStatus
		want   string
	}{
		{"pending", api.AgentStatusPending, "pending"},
		{"creating", api.AgentStatusCreating, "creating"},
		{"running", api.AgentStatusRunning, "running"},
		{"stopping", api.AgentStatusStopping, "stopping"},
		{"stopped", api.AgentStatusStopped, "stopped"},
		{"failed", api.AgentStatusFailed, "failed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.status) != tt.want {
				t.Errorf("status = %v, want %v", tt.status, tt.want)
			}
		})
	}
}

func TestResourceLimits(t *testing.T) {
	limits := api.ResourceLimits{
		CPUCount: 2,
		MemoryMB: 1024,
		DiskMB:   10240,
	}

	if limits.CPUCount != 2 {
		t.Errorf("CPUCount = %d, want 2", limits.CPUCount)
	}
	if limits.MemoryMB != 1024 {
		t.Errorf("MemoryMB = %d, want 1024", limits.MemoryMB)
	}
	if limits.DiskMB != 10240 {
		t.Errorf("DiskMB = %d, want 10240", limits.DiskMB)
	}
}

func TestAgentConfig(t *testing.T) {
	config := api.AgentConfig{
		ID:       api.AgentID("test-agent-1"),
		TenantID: api.TenantID("tenant-1"),
		Name:     "Test Agent",
		Image:    "python:3.11",
		Resources: api.ResourceLimits{
			CPUCount: 1,
			MemoryMB: 512,
		},
	}

	if config.ID != "test-agent-1" {
		t.Errorf("ID = %s, want test-agent-1", config.ID)
	}
	if config.TenantID != "tenant-1" {
		t.Errorf("TenantID = %s, want tenant-1", config.TenantID)
	}
	if config.Name != "Test Agent" {
		t.Errorf("Name = %s, want Test Agent", config.Name)
	}
	if config.Image != "python:3.11" {
		t.Errorf("Image = %s, want python:3.11", config.Image)
	}
}
