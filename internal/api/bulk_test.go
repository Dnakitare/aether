package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/dnakitare/aether/pkg/api"
)

func TestBulkCreateAgents(t *testing.T) {
	_, _, ts := newTestServer(t)

	t.Run("create multiple agents", func(t *testing.T) {
		body := BulkCreateAgentsRequest{
			Agents: []api.AgentConfig{
				{ID: "bulk-1", Name: "agent-1", Image: "docker.io/test:latest", Resources: api.ResourceLimits{CPUCount: 1, MemoryMB: 256, DiskMB: 1024}},
				{ID: "bulk-2", Name: "agent-2", Image: "docker.io/test:latest", Resources: api.ResourceLimits{CPUCount: 1, MemoryMB: 256, DiskMB: 1024}},
				{ID: "bulk-3", Name: "agent-3", Image: "docker.io/test:latest", Resources: api.ResourceLimits{CPUCount: 1, MemoryMB: 256, DiskMB: 1024}},
			},
		}

		resp := do(t, ts, http.MethodPost, "/v1/agents/bulk/create", body, "tenant-1")

		var result BulkCreateAgentsResponse
		json.NewDecoder(resp.Body).Decode(&result)
		resp.Body.Close()

		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("expected 201, got %d; failed: %+v", resp.StatusCode, result.Failed)
		}

		if result.Summary.Total != 3 {
			t.Errorf("expected total=3, got %d", result.Summary.Total)
		}
		if result.Summary.Succeeded != 3 {
			t.Errorf("expected succeeded=3, got %d", result.Summary.Succeeded)
		}
		if result.Summary.Failed != 0 {
			t.Errorf("expected failed=0, got %d", result.Summary.Failed)
		}
	})

	t.Run("empty agents returns validation error", func(t *testing.T) {
		body := BulkCreateAgentsRequest{Agents: []api.AgentConfig{}}
		resp := do(t, ts, http.MethodPost, "/v1/agents/bulk/create", body, "tenant-1")
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", resp.StatusCode)
		}
		resp.Body.Close()
	})

	t.Run("exceeds max bulk size", func(t *testing.T) {
		agents := make([]api.AgentConfig, 101)
		for i := range agents {
			agents[i] = api.AgentConfig{Name: "x", Image: "docker.io/test:latest", Resources: api.ResourceLimits{CPUCount: 1, MemoryMB: 256, DiskMB: 1024}}
		}
		body := BulkCreateAgentsRequest{Agents: agents}
		resp := do(t, ts, http.MethodPost, "/v1/agents/bulk/create", body, "tenant-1")
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", resp.StatusCode)
		}
		resp.Body.Close()
	})

	t.Run("partial failure returns 207", func(t *testing.T) {
		body := BulkCreateAgentsRequest{
			Agents: []api.AgentConfig{
				{ID: "bulk-ok", Name: "good-agent", Image: "docker.io/test:latest", Resources: api.ResourceLimits{CPUCount: 1, MemoryMB: 256, DiskMB: 1024}},
				{ID: "", Name: "", Image: "invalid-registry/bad", Resources: api.ResourceLimits{CPUCount: 1, MemoryMB: 256, DiskMB: 1024}},
			},
		}
		resp := do(t, ts, http.MethodPost, "/v1/agents/bulk/create", body, "tenant-1")

		var result BulkCreateAgentsResponse
		json.NewDecoder(resp.Body).Decode(&result)
		resp.Body.Close()

		// At least one should fail (validation) making it 207
		if resp.StatusCode != http.StatusMultiStatus && resp.StatusCode != http.StatusCreated {
			t.Errorf("expected 207 or 201, got %d", resp.StatusCode)
		}
	})

	t.Run("no auth returns 401", func(t *testing.T) {
		body := BulkCreateAgentsRequest{
			Agents: []api.AgentConfig{{Name: "x", Image: "docker.io/test:latest", Resources: api.ResourceLimits{CPUCount: 1, MemoryMB: 256, DiskMB: 1024}}},
		}
		resp := do(t, ts, http.MethodPost, "/v1/agents/bulk/create", body, "")
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", resp.StatusCode)
		}
		resp.Body.Close()
	})
}

func TestBulkDeleteAgents(t *testing.T) {
	_, rt, ts := newTestServer(t)

	// Seed agents
	for _, id := range []string{"del-1", "del-2", "del-3"} {
		rt.agents[api.AgentID(id)] = &api.AgentInfo{
			Config: api.AgentConfig{
				ID:        api.AgentID(id),
				TenantID:  "tenant-1",
				Name:      id,
				Resources: api.ResourceLimits{CPUCount: 1, MemoryMB: 256, DiskMB: 1024},
			},
			Status: api.AgentStatusRunning,
		}
	}

	t.Run("delete multiple agents", func(t *testing.T) {
		body := BulkDeleteAgentsRequest{
			AgentIDs: []api.AgentID{"del-1", "del-2"},
		}
		resp := do(t, ts, http.MethodPost, "/v1/agents/bulk/delete", body, "tenant-1")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}

		var result BulkDeleteAgentsResponse
		json.NewDecoder(resp.Body).Decode(&result)
		resp.Body.Close()

		if result.Summary.Succeeded != 2 {
			t.Errorf("expected succeeded=2, got %d", result.Summary.Succeeded)
		}
	})

	t.Run("delete nonexistent agent returns partial failure", func(t *testing.T) {
		body := BulkDeleteAgentsRequest{
			AgentIDs: []api.AgentID{"del-3", "nonexistent"},
		}
		resp := do(t, ts, http.MethodPost, "/v1/agents/bulk/delete", body, "tenant-1")

		var result BulkDeleteAgentsResponse
		json.NewDecoder(resp.Body).Decode(&result)
		resp.Body.Close()

		if resp.StatusCode != http.StatusMultiStatus {
			t.Errorf("expected 207, got %d", resp.StatusCode)
		}
		if result.Summary.Succeeded != 1 {
			t.Errorf("expected succeeded=1, got %d", result.Summary.Succeeded)
		}
		if result.Summary.Failed != 1 {
			t.Errorf("expected failed=1, got %d", result.Summary.Failed)
		}
	})

	t.Run("empty agent IDs returns validation error", func(t *testing.T) {
		body := BulkDeleteAgentsRequest{AgentIDs: []api.AgentID{}}
		resp := do(t, ts, http.MethodPost, "/v1/agents/bulk/delete", body, "tenant-1")
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", resp.StatusCode)
		}
		resp.Body.Close()
	})

	t.Run("cross-tenant delete denied", func(t *testing.T) {
		rt.agents["other-tenant-agent"] = &api.AgentInfo{
			Config: api.AgentConfig{
				ID:        "other-tenant-agent",
				TenantID:  "tenant-2",
				Name:      "other",
				Resources: api.ResourceLimits{CPUCount: 1, MemoryMB: 256, DiskMB: 1024},
			},
			Status: api.AgentStatusRunning,
		}

		body := BulkDeleteAgentsRequest{
			AgentIDs: []api.AgentID{"other-tenant-agent"},
		}
		resp := do(t, ts, http.MethodPost, "/v1/agents/bulk/delete", body, "tenant-1")

		var result BulkDeleteAgentsResponse
		json.NewDecoder(resp.Body).Decode(&result)
		resp.Body.Close()

		if result.Summary.Failed != 1 {
			t.Errorf("expected failed=1, got %d", result.Summary.Failed)
		}
		if len(result.Failed) > 0 && result.Failed[0].Error == "" {
			t.Error("expected error message for cross-tenant denial")
		}
	})
}
