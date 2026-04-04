package api

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dnakitare/aether/internal/auth"
	"github.com/dnakitare/aether/internal/recovery"
	"github.com/dnakitare/aether/pkg/api"
)

// mockCheckpointRuntime implements both api.Runtime and CheckpointRuntime so
// that New() wires up the checkpoint routes automatically.
type mockCheckpointRuntime struct {
	mockRuntime
	checkpoints map[api.AgentID][]*recovery.Checkpoint
	restoreErr  error
	deleteErr   error
}

func newMockCheckpointRuntime() *mockCheckpointRuntime {
	return &mockCheckpointRuntime{
		mockRuntime: mockRuntime{agents: make(map[api.AgentID]*api.AgentInfo)},
		checkpoints: make(map[api.AgentID][]*recovery.Checkpoint),
	}
}

func (m *mockCheckpointRuntime) CreateCheckpoint(_ context.Context, agentID api.AgentID) (*recovery.Checkpoint, error) {
	cp := &recovery.Checkpoint{
		AgentID:   agentID,
		Version:   len(m.checkpoints[agentID]) + 1,
		CreatedAt: time.Now(),
	}
	m.checkpoints[agentID] = append(m.checkpoints[agentID], cp)
	return cp, nil
}

func (m *mockCheckpointRuntime) ListCheckpoints(_ context.Context, agentID api.AgentID) ([]*recovery.Checkpoint, error) {
	return m.checkpoints[agentID], nil
}

func (m *mockCheckpointRuntime) GetLatestCheckpoint(_ context.Context, agentID api.AgentID) (*recovery.Checkpoint, error) {
	cps := m.checkpoints[agentID]
	if len(cps) == 0 {
		return nil, fmt.Errorf("no checkpoints for %s", agentID)
	}
	return cps[len(cps)-1], nil
}

func (m *mockCheckpointRuntime) RestoreFromCheckpoint(_ context.Context, _ api.AgentID, _ int) error {
	return m.restoreErr
}

func (m *mockCheckpointRuntime) DeleteCheckpoint(_ context.Context, agentID api.AgentID, version int) error {
	if m.deleteErr != nil {
		return m.deleteErr
	}
	cps := m.checkpoints[agentID]
	for i, cp := range cps {
		if cp.Version == version {
			m.checkpoints[agentID] = append(cps[:i], cps[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("checkpoint version %d not found", version)
}

// newCheckpointTestServer is like newTestServer but backed by mockCheckpointRuntime
// so that checkpoint routes are registered.
func newCheckpointTestServer(t *testing.T) (*Server, *mockCheckpointRuntime, *httptest.Server) {
	t.Helper()
	rt := newMockCheckpointRuntime()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := Config{
		Address:      ":0",
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
		EnableAuth:   false,
	}
	srv := New(logger, cfg, rt, nil, nil, nil, nil)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if tenant := r.Header.Get("X-Test-Tenant"); tenant != "" {
			claims := &auth.Claims{
				TenantID: api.TenantID(tenant),
				UserID:   "test-user",
				Role:     r.Header.Get("X-Test-Role"),
			}
			r = r.WithContext(auth.WithClaims(r.Context(), claims))
		}
		srv.Router().ServeHTTP(w, r)
	}))
	t.Cleanup(ts.Close)
	return srv, rt, ts
}

// doCheckpoint performs a request using the checkpoint test server.
func doCheckpoint(t *testing.T, ts *httptest.Server, method, path string, tenantID string) *http.Response {
	t.Helper()
	return do(t, ts, method, path, nil, tenantID)
}

// --- checkpoint version validation tests ------------------------------------

func TestRestoreCheckpoint_ZeroVersion(t *testing.T) {
	_, rt, ts := newCheckpointTestServer(t)
	rt.agents["agent-1"] = &api.AgentInfo{
		Config: api.AgentConfig{ID: "agent-1", TenantID: "tenant-1"},
		Status: api.AgentStatusRunning,
	}

	resp := doCheckpoint(t, ts, http.MethodPost, "/v1/agents/agent-1/checkpoints/0/restore", "tenant-1")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("want 400 for version=0, got %d", resp.StatusCode)
	}
}

func TestRestoreCheckpoint_NegativeVersion(t *testing.T) {
	_, rt, ts := newCheckpointTestServer(t)
	rt.agents["agent-1"] = &api.AgentInfo{
		Config: api.AgentConfig{ID: "agent-1", TenantID: "tenant-1"},
		Status: api.AgentStatusRunning,
	}

	resp := doCheckpoint(t, ts, http.MethodPost, "/v1/agents/agent-1/checkpoints/-1/restore", "tenant-1")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("want 400 for version=-1, got %d", resp.StatusCode)
	}
}

func TestDeleteCheckpoint_ZeroVersion(t *testing.T) {
	_, rt, ts := newCheckpointTestServer(t)
	rt.agents["agent-1"] = &api.AgentInfo{
		Config: api.AgentConfig{ID: "agent-1", TenantID: "tenant-1"},
		Status: api.AgentStatusRunning,
	}

	resp := doCheckpoint(t, ts, http.MethodDelete, "/v1/agents/agent-1/checkpoints/0", "tenant-1")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("want 400 for version=0, got %d", resp.StatusCode)
	}
}

func TestDeleteCheckpoint_NegativeVersion(t *testing.T) {
	_, rt, ts := newCheckpointTestServer(t)
	rt.agents["agent-1"] = &api.AgentInfo{
		Config: api.AgentConfig{ID: "agent-1", TenantID: "tenant-1"},
		Status: api.AgentStatusRunning,
	}

	resp := doCheckpoint(t, ts, http.MethodDelete, "/v1/agents/agent-1/checkpoints/-1", "tenant-1")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("want 400 for version=-1, got %d", resp.StatusCode)
	}
}

func TestRestoreCheckpoint_ValidVersion(t *testing.T) {
	_, rt, ts := newCheckpointTestServer(t)
	rt.agents["agent-1"] = &api.AgentInfo{
		Config: api.AgentConfig{ID: "agent-1", TenantID: "tenant-1"},
		Status: api.AgentStatusRunning,
	}

	resp := doCheckpoint(t, ts, http.MethodPost, "/v1/agents/agent-1/checkpoints/1/restore", "tenant-1")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
}

func TestListCheckpoints_CrossTenantForbidden(t *testing.T) {
	_, rt, ts := newCheckpointTestServer(t)
	rt.agents["agent-t2"] = &api.AgentInfo{
		Config: api.AgentConfig{ID: "agent-t2", TenantID: "tenant-2"},
		Status: api.AgentStatusRunning,
	}

	resp := doCheckpoint(t, ts, http.MethodGet, "/v1/agents/agent-t2/checkpoints", "tenant-1")
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("want 403, got %d", resp.StatusCode)
	}
}

func TestCreateCheckpoint_Success(t *testing.T) {
	_, rt, ts := newCheckpointTestServer(t)
	rt.agents["agent-1"] = &api.AgentInfo{
		Config: api.AgentConfig{ID: "agent-1", TenantID: "tenant-1"},
		Status: api.AgentStatusRunning,
	}

	resp := doCheckpoint(t, ts, http.MethodPost, "/v1/agents/agent-1/checkpoints", "tenant-1")
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("want 201, got %d", resp.StatusCode)
	}

	var cp recovery.Checkpoint
	decodeJSON(t, resp, &cp)
	if cp.Version != 1 {
		t.Errorf("want checkpoint version 1, got %d", cp.Version)
	}
}
