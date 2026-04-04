package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dnakitare/aether/internal/auth"
	"github.com/dnakitare/aether/pkg/api"
)

// --- mock runtime --------------------------------------------------------

type mockRuntime struct {
	agents           map[api.AgentID]*api.AgentInfo
	GetAgentLogsFunc func(context.Context, api.AgentID, bool) (io.ReadCloser, error)
}

func newMockRuntime() *mockRuntime {
	return &mockRuntime{agents: make(map[api.AgentID]*api.AgentInfo)}
}

func (m *mockRuntime) CreateAgent(_ context.Context, config api.AgentConfig) error {
	m.agents[config.ID] = &api.AgentInfo{
		Config:    config,
		Status:    api.AgentStatusPending,
		CreatedAt: time.Now(),
	}
	return nil
}

func (m *mockRuntime) StartAgent(_ context.Context, id api.AgentID) error {
	a, ok := m.agents[id]
	if !ok {
		return fmt.Errorf("agent %s not found", id)
	}
	a.Status = api.AgentStatusRunning
	return nil
}

func (m *mockRuntime) StopAgent(_ context.Context, id api.AgentID, _ time.Duration) error {
	a, ok := m.agents[id]
	if !ok {
		return fmt.Errorf("agent %s not found", id)
	}
	a.Status = api.AgentStatusStopped
	return nil
}

func (m *mockRuntime) DestroyAgent(_ context.Context, id api.AgentID) error {
	if _, ok := m.agents[id]; !ok {
		return fmt.Errorf("agent %s not found", id)
	}
	delete(m.agents, id)
	return nil
}

func (m *mockRuntime) GetAgent(_ context.Context, id api.AgentID) (*api.AgentInfo, error) {
	a, ok := m.agents[id]
	if !ok {
		return nil, fmt.Errorf("agent %s not found", id)
	}
	return a, nil
}

func (m *mockRuntime) ListAgents(_ context.Context, tenantID *api.TenantID) ([]*api.AgentInfo, error) {
	var result []*api.AgentInfo
	for _, a := range m.agents {
		if tenantID == nil || a.Config.TenantID == *tenantID {
			result = append(result, a)
		}
	}
	return result, nil
}

func (m *mockRuntime) GetAgentLogs(ctx context.Context, id api.AgentID, follow bool) (io.ReadCloser, error) {
	if m.GetAgentLogsFunc != nil {
		return m.GetAgentLogsFunc(ctx, id, follow)
	}
	return io.NopCloser(bytes.NewBufferString("log line\n")), nil
}

func (m *mockRuntime) GetAgentHealth(_ context.Context, id api.AgentID) (*api.HealthStatus, error) {
	if _, ok := m.agents[id]; !ok {
		return nil, fmt.Errorf("agent %s not found", id)
	}
	return &api.HealthStatus{Healthy: true, CheckedAt: time.Now()}, nil
}

func (m *mockRuntime) Shutdown(_ context.Context) error { return nil }

// --- test infrastructure -------------------------------------------------

// newTestServer builds a Server with auth disabled and a mock runtime.
// It returns the server and an httptest.Server that routes through the real mux.
func newTestServer(t *testing.T) (*Server, *mockRuntime, *httptest.Server) {
	t.Helper()
	rt := newMockRuntime()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := Config{
		Address:      ":0",
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
		EnableAuth:   false, // auth middleware skipped; we inject claims manually
	}
	srv := New(logger, cfg, rt, nil, nil, nil, nil)

	// Wrap the router with a middleware that reads claims from a request header
	// we set in tests. This lets us inject auth context without a real JWT.
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

// do performs an HTTP request against the test server with optional tenant header.
func do(t *testing.T, ts *httptest.Server, method, path string, body interface{}, tenantID string) *http.Response {
	t.Helper()
	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		bodyReader = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, ts.URL+path, bodyReader)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if tenantID != "" {
		req.Header.Set("X-Test-Tenant", tenantID)
		req.Header.Set("X-Test-Role", "developer")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("http do: %v", err)
	}
	return resp
}

func decodeJSON(t *testing.T, resp *http.Response, v interface{}) {
	t.Helper()
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		t.Fatalf("decode response body: %v", err)
	}
}

// --- agent handler tests -------------------------------------------------

func TestListAgents_Empty(t *testing.T) {
	_, _, ts := newTestServer(t)

	resp := do(t, ts, http.MethodGet, "/v1/agents", nil, "tenant-1")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
}

func TestListAgents_TenantIsolation(t *testing.T) {
	_, rt, ts := newTestServer(t)

	rt.agents["agent-t1"] = &api.AgentInfo{
		Config: api.AgentConfig{ID: "agent-t1", TenantID: "tenant-1", Name: "a1"},
		Status: api.AgentStatusRunning,
	}
	rt.agents["agent-t2"] = &api.AgentInfo{
		Config: api.AgentConfig{ID: "agent-t2", TenantID: "tenant-2", Name: "a2"},
		Status: api.AgentStatusRunning,
	}

	resp := do(t, ts, http.MethodGet, "/v1/agents", nil, "tenant-1")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}

	var result struct {
		Pagination struct {
			TotalItems int `json:"total_items"`
		} `json:"pagination"`
	}
	decodeJSON(t, resp, &result)
	if result.Pagination.TotalItems != 1 {
		t.Errorf("want 1 agent for tenant-1, got %d", result.Pagination.TotalItems)
	}
}

func TestListAgents_NoAuth(t *testing.T) {
	_, _, ts := newTestServer(t)
	// No tenant header → no claims in context.
	resp := do(t, ts, http.MethodGet, "/v1/agents", nil, "")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", resp.StatusCode)
	}
}

func TestCreateAgent_Success(t *testing.T) {
	_, rt, ts := newTestServer(t)

	body := api.AgentConfig{
		Name:  "test-agent",
		Image: "docker.io/library/ubuntu:22.04",
		Resources: api.ResourceLimits{
			CPUCount: 1,
			MemoryMB: 512,
			DiskMB:   1024,
		},
	}

	resp := do(t, ts, http.MethodPost, "/v1/agents", body, "tenant-1")
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("want 201, got %d: %s", resp.StatusCode, b)
	}

	var info api.AgentInfo
	decodeJSON(t, resp, &info)
	if info.Config.TenantID != "tenant-1" {
		t.Errorf("want tenant-1, got %s", info.Config.TenantID)
	}
	if _, exists := rt.agents[info.Config.ID]; !exists {
		t.Error("agent not found in runtime after create")
	}
}

func TestCreateAgent_CrossTenantRejected(t *testing.T) {
	_, _, ts := newTestServer(t)

	body := api.AgentConfig{
		Name:     "sneaky",
		Image:    "docker.io/library/ubuntu:22.04",
		TenantID: "other-tenant", // mismatches auth tenant
		Resources: api.ResourceLimits{
			CPUCount: 1,
			MemoryMB: 512,
			DiskMB:   1024,
		},
	}

	resp := do(t, ts, http.MethodPost, "/v1/agents", body, "tenant-1")
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("want 403, got %d", resp.StatusCode)
	}
}

func TestCreateAgent_InvalidJSON(t *testing.T) {
	_, _, ts := newTestServer(t)

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/v1/agents", bytes.NewBufferString("{bad"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Test-Tenant", "tenant-1")
	req.Header.Set("X-Test-Role", "developer")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", resp.StatusCode)
	}
}

func TestGetAgent_Success(t *testing.T) {
	_, rt, ts := newTestServer(t)

	rt.agents["agent-1"] = &api.AgentInfo{
		Config: api.AgentConfig{ID: "agent-1", TenantID: "tenant-1"},
		Status: api.AgentStatusRunning,
	}

	resp := do(t, ts, http.MethodGet, "/v1/agents/agent-1", nil, "tenant-1")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
}

func TestGetAgent_CrossTenantForbidden(t *testing.T) {
	_, rt, ts := newTestServer(t)

	rt.agents["agent-t2"] = &api.AgentInfo{
		Config: api.AgentConfig{ID: "agent-t2", TenantID: "tenant-2"},
		Status: api.AgentStatusRunning,
	}

	resp := do(t, ts, http.MethodGet, "/v1/agents/agent-t2", nil, "tenant-1")
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("want 403, got %d", resp.StatusCode)
	}
}

func TestGetAgent_NotFound(t *testing.T) {
	_, _, ts := newTestServer(t)

	resp := do(t, ts, http.MethodGet, "/v1/agents/ghost", nil, "tenant-1")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("want 404, got %d", resp.StatusCode)
	}
}

func TestDeleteAgent_Success(t *testing.T) {
	_, rt, ts := newTestServer(t)

	rt.agents["agent-1"] = &api.AgentInfo{
		Config: api.AgentConfig{ID: "agent-1", TenantID: "tenant-1"},
		Status: api.AgentStatusStopped,
	}

	resp := do(t, ts, http.MethodDelete, "/v1/agents/agent-1", nil, "tenant-1")
	if resp.StatusCode != http.StatusNoContent {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("want 204, got %d: %s", resp.StatusCode, b)
	}
	if _, exists := rt.agents["agent-1"]; exists {
		t.Error("agent should have been deleted")
	}
}

func TestDeleteAgent_CrossTenantForbidden(t *testing.T) {
	_, rt, ts := newTestServer(t)

	rt.agents["agent-t2"] = &api.AgentInfo{
		Config: api.AgentConfig{ID: "agent-t2", TenantID: "tenant-2"},
		Status: api.AgentStatusRunning,
	}

	resp := do(t, ts, http.MethodDelete, "/v1/agents/agent-t2", nil, "tenant-1")
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("want 403, got %d", resp.StatusCode)
	}
	if _, exists := rt.agents["agent-t2"]; !exists {
		t.Error("agent should NOT have been deleted")
	}
}

func TestGetAgentHealth_Success(t *testing.T) {
	_, rt, ts := newTestServer(t)

	rt.agents["agent-1"] = &api.AgentInfo{
		Config: api.AgentConfig{ID: "agent-1", TenantID: "tenant-1"},
		Status: api.AgentStatusRunning,
	}

	resp := do(t, ts, http.MethodGet, "/v1/agents/agent-1/health", nil, "tenant-1")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
}

func TestGetAgentHealth_CrossTenantForbidden(t *testing.T) {
	_, rt, ts := newTestServer(t)

	rt.agents["agent-t2"] = &api.AgentInfo{
		Config: api.AgentConfig{ID: "agent-t2", TenantID: "tenant-2"},
		Status: api.AgentStatusRunning,
	}

	resp := do(t, ts, http.MethodGet, "/v1/agents/agent-t2/health", nil, "tenant-1")
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("want 403, got %d", resp.StatusCode)
	}
}

// --- health endpoint tests -----------------------------------------------

func TestHealthEndpoint(t *testing.T) {
	_, _, ts := newTestServer(t)

	resp := do(t, ts, http.MethodGet, "/health", nil, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
}

func TestReadinessEndpoint(t *testing.T) {
	_, _, ts := newTestServer(t)

	resp := do(t, ts, http.MethodGet, "/readiness", nil, "")
	// No health checks registered in test server, so should return healthy.
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
}

// --- log streaming tests ----------------------------------------------------

func TestGetAgentLogs_NonSSE_WritesBody(t *testing.T) {
	_, rt, ts := newTestServer(t)

	rt.agents["agent-1"] = &api.AgentInfo{
		Config: api.AgentConfig{ID: "agent-1", TenantID: "tenant-1"},
		Status: api.AgentStatusRunning,
	}

	resp := do(t, ts, http.MethodGet, "/v1/agents/agent-1/logs", nil, "tenant-1")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if string(body) != "log line\n" {
		t.Errorf("want body 'log line\\n', got %q", body)
	}
}

func TestGetAgentLogs_NonSSE_ContentType(t *testing.T) {
	_, rt, ts := newTestServer(t)

	rt.agents["agent-1"] = &api.AgentInfo{
		Config: api.AgentConfig{ID: "agent-1", TenantID: "tenant-1"},
		Status: api.AgentStatusRunning,
	}

	resp := do(t, ts, http.MethodGet, "/v1/agents/agent-1/logs", nil, "tenant-1")
	resp.Body.Close()
	ct := resp.Header.Get("Content-Type")
	if ct != "text/plain" {
		t.Errorf("want Content-Type 'text/plain', got %q", ct)
	}
}

func TestGetAgentLogs_SSE_NewlineEscaping(t *testing.T) {
	_, rt, ts := newTestServer(t)

	// Return a log line that itself contains an embedded newline.
	rt.GetAgentLogsFunc = func(_ context.Context, _ api.AgentID, _ bool) (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewBufferString("line one\nline two\n")), nil
	}

	rt.agents["agent-1"] = &api.AgentInfo{
		Config: api.AgentConfig{ID: "agent-1", TenantID: "tenant-1"},
		Status: api.AgentStatusRunning,
	}

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/v1/agents/agent-1/logs?follow=true", nil)
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("X-Test-Tenant", "tenant-1")
	req.Header.Set("X-Test-Role", "developer")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	// The scanner reads line-by-line, so "line one" and "line two" are separate
	// SSE frames — each should appear as "data: <text>\n\n".
	got := string(body)
	if !bytes.Contains(body, []byte("data: line one\n\n")) {
		t.Errorf("want SSE frame for 'line one', got:\n%s", got)
	}
	if !bytes.Contains(body, []byte("data: line two\n\n")) {
		t.Errorf("want SSE frame for 'line two', got:\n%s", got)
	}
}

func TestGetAgentLogs_CrossTenantForbidden(t *testing.T) {
	_, rt, ts := newTestServer(t)

	rt.agents["agent-t2"] = &api.AgentInfo{
		Config: api.AgentConfig{ID: "agent-t2", TenantID: "tenant-2"},
		Status: api.AgentStatusRunning,
	}

	resp := do(t, ts, http.MethodGet, "/v1/agents/agent-t2/logs", nil, "tenant-1")
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("want 403, got %d", resp.StatusCode)
	}
}
