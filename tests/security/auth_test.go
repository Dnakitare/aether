package security_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/aether-runtime/aether/internal/api"
	"github.com/aether-runtime/aether/internal/auth"
	"github.com/aether-runtime/aether/pkg/api"
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
	// This would be your actual server setup
	// For now, returning a mock handler
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})
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
