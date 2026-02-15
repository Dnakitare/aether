package security_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/aether-runtime/aether/internal/api"
	"github.com/aether-runtime/aether/internal/auth"
	"github.com/aether-runtime/aether/internal/scaler"
	"github.com/aether-runtime/aether/internal/scheduler"
	"github.com/aether-runtime/aether/internal/tenant"
	pkgapi "github.com/aether-runtime/aether/pkg/api"
)

// TestAuthenticationRequired verifies that all API endpoints require authentication.
func TestAuthenticationRequired(t *testing.T) {
	// Setup mock server
	server := setupTestServer(t)

	tests := []struct {
		name           string
		method         string
		path           string
		expectedStatus int
	}{
		{"GET agents without auth", "GET", "/v1/agents", http.StatusUnauthorized},
		{"POST agents without auth", "POST", "/v1/agents", http.StatusUnauthorized},
		{"GET agent by ID without auth", "GET", "/v1/agents/test-123", http.StatusUnauthorized},
		{"DELETE agent without auth", "DELETE", "/v1/agents/test-123", http.StatusUnauthorized},
		{"GET agent logs without auth", "GET", "/v1/agents/test-123/logs", http.StatusUnauthorized},
		{"GET quotas without auth", "GET", "/v1/quotas", http.StatusUnauthorized},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			w := httptest.NewRecorder()

			server.ServeHTTP(w, req)

			if w.Code != tt.expectedStatus {
				t.Errorf("expected status %d, got %d", tt.expectedStatus, w.Code)
			}
		})
	}
}

// TestInvalidTokenRejected verifies that invalid tokens are rejected.
func TestInvalidTokenRejected(t *testing.T) {
	server := setupTestServer(t)

	tests := []struct {
		name           string
		authHeader     string
		expectedStatus int
	}{
		{"Missing Bearer prefix", "invalid-token", http.StatusUnauthorized},
		{"Empty token", "Bearer ", http.StatusUnauthorized},
		{"Malformed token", "Bearer not.a.jwt", http.StatusUnauthorized},
		{"Expired token", "Bearer " + generateExpiredToken(t), http.StatusUnauthorized},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/v1/agents", nil)
			req.Header.Set("Authorization", tt.authHeader)
			w := httptest.NewRecorder()

			server.ServeHTTP(w, req)

			if w.Code != tt.expectedStatus {
				t.Errorf("expected status %d, got %d", tt.expectedStatus, w.Code)
			}
		})
	}
}

// TestValidTokenAccepted verifies that valid tokens are accepted.
func TestValidTokenAccepted(t *testing.T) {
	server := setupTestServer(t)

	// Generate valid token
	jwtMgr, err := auth.NewJWTManager(auth.Config{
		SecretKey:     "test-secret-key-min-32-chars-long!!",
		TokenDuration: 1 * time.Hour,
	})
	if err != nil {
		t.Fatalf("failed to create JWT manager: %v", err)
	}

	token, err := jwtMgr.GenerateToken("tenant-123", "user-123", "admin")
	if err != nil {
		t.Fatalf("failed to generate token: %v", err)
	}

	req := httptest.NewRequest("GET", "/v1/agents", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	// Should NOT be 401 with valid token
	if w.Code == http.StatusUnauthorized {
		t.Errorf("valid token was rejected")
	}
}

// TestHealthEndpointsUnauthenticated verifies health endpoints don't require auth.
func TestHealthEndpointsUnauthenticated(t *testing.T) {
	server := setupTestServer(t)

	tests := []struct {
		name string
		path string
	}{
		{"Health endpoint", "/health"},
		{"Readiness endpoint", "/readiness"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", tt.path, nil)
			w := httptest.NewRecorder()

			server.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Errorf("health endpoint should not require auth, got status %d", w.Code)
			}
		})
	}
}

// Helper functions

func setupTestServer(t *testing.T) http.Handler {
	// Create logger
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelError, // Reduce noise in tests
	}))

	// Create JWT manager
	jwtMgr, err := auth.NewJWTManager(auth.Config{
		SecretKey:     "test-secret-key-min-32-chars-long!!",
		TokenDuration: 1 * time.Hour,
	})
	if err != nil {
		t.Fatalf("failed to create JWT manager: %v", err)
	}

	// Create mock scheduler
	schedConfig := scheduler.Config{
		Strategy:        scheduler.BinPacking,
		Interval:        1 * time.Second,
		EventBufferSize: 10,
	}
	sched := scheduler.New(logger, schedConfig)

	// Create mock metrics provider and scale executor for scaler
	var metricsProvider mockMetricsProvider
	var executor mockScaleExecutor

	// Create mock scaler
	scalerConfig := scaler.Config{
		Interval:        30 * time.Second,
		DefaultCooldown: 5 * time.Minute,
	}
	sc := scaler.New(logger, scalerConfig, &metricsProvider, &executor)

	// Create mock quota manager (nil is acceptable for these tests)
	var qm *tenant.QuotaManager

	// Create mock runtime
	var runtime mockRuntime

	// Create API server with authentication enabled
	serverConfig := api.Config{
		Address:      ":8080",
		EnableAuth:   true, // Enable authentication
		EnableCORS:   false,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
	}

	server := api.New(logger, serverConfig, &runtime, sched, sc, qm, jwtMgr)

	// Return the router which implements http.Handler
	return server.Router()
}

// mockRuntime is a minimal runtime implementation for testing
type mockRuntime struct{}

func (m *mockRuntime) CreateAgent(ctx context.Context, config pkgapi.AgentConfig) error {
	return nil
}

func (m *mockRuntime) StartAgent(ctx context.Context, id pkgapi.AgentID) error {
	return nil
}

func (m *mockRuntime) StopAgent(ctx context.Context, id pkgapi.AgentID, timeout time.Duration) error {
	return nil
}

func (m *mockRuntime) DestroyAgent(ctx context.Context, id pkgapi.AgentID) error {
	return nil
}

func (m *mockRuntime) GetAgent(ctx context.Context, id pkgapi.AgentID) (*pkgapi.AgentInfo, error) {
	return nil, nil
}

func (m *mockRuntime) ListAgents(ctx context.Context, tenantID *pkgapi.TenantID) ([]*pkgapi.AgentInfo, error) {
	return []*pkgapi.AgentInfo{}, nil
}

func (m *mockRuntime) GetAgentLogs(ctx context.Context, id pkgapi.AgentID, follow bool) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("")), nil
}

func (m *mockRuntime) GetAgentHealth(ctx context.Context, id pkgapi.AgentID) (*pkgapi.HealthStatus, error) {
	return &pkgapi.HealthStatus{Healthy: true}, nil
}

func (m *mockRuntime) Shutdown(ctx context.Context) error {
	return nil
}

// mockMetricsProvider is a minimal metrics provider for testing
type mockMetricsProvider struct{}

func (m *mockMetricsProvider) GetAgentMetrics(ctx context.Context, agentID pkgapi.AgentID) (*pkgapi.AgentMetrics, error) {
	return nil, nil
}

func (m *mockMetricsProvider) GetTenantMetrics(ctx context.Context, tenantID pkgapi.TenantID) (*scaler.TenantMetrics, error) {
	return nil, nil
}

// mockScaleExecutor is a minimal scale executor for testing
type mockScaleExecutor struct{}

func (m *mockScaleExecutor) ScaleUp(ctx context.Context, target scaler.ScaleTarget, count int) error {
	return nil
}

func (m *mockScaleExecutor) ScaleDown(ctx context.Context, target scaler.ScaleTarget, count int) error {
	return nil
}

func generateExpiredToken(t *testing.T) string {
	jwtMgr, err := auth.NewJWTManager(auth.Config{
		SecretKey:     "test-secret-key-min-32-chars-long!!",
		TokenDuration: -1 * time.Hour, // Negative duration = already expired
	})
	if err != nil {
		t.Fatalf("failed to create JWT manager: %v", err)
	}

	token, err := jwtMgr.GenerateToken("tenant-123", "user-123", "admin")
	if err != nil {
		t.Fatalf("failed to generate expired token: %v", err)
	}

	return token
}
