package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
)

// mockTokenIssuer is a test double for TokenIssuer.
type mockTokenIssuer struct {
	token string
	err   error
}

func (m *mockTokenIssuer) IssueToken(_ context.Context, _ string) (string, error) {
	return m.token, m.err
}

func TestHandleIssueToken_NoIssuerConfigured(t *testing.T) {
	_, _, ts := newTestServer(t)
	// newTestServer creates a server with no token issuer → 503.
	resp := do(t, ts, http.MethodPost, "/v1/auth/token", map[string]string{"api_key": "key123"}, "")
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("want 503, got %d", resp.StatusCode)
	}
}

func TestHandleIssueToken_MissingAPIKey(t *testing.T) {
	srv, _, ts := newTestServer(t)
	srv.WithTokenIssuer(&mockTokenIssuer{token: "tok"})

	// Empty api_key field.
	resp := do(t, ts, http.MethodPost, "/v1/auth/token", map[string]string{"api_key": ""}, "")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", resp.StatusCode)
	}
}

func TestHandleIssueToken_InvalidJSON(t *testing.T) {
	srv, _, ts := newTestServer(t)
	srv.WithTokenIssuer(&mockTokenIssuer{token: "tok"})

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/v1/auth/token", strings.NewReader("{bad"))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", resp.StatusCode)
	}
}

func TestHandleIssueToken_InvalidKey(t *testing.T) {
	srv, _, ts := newTestServer(t)
	srv.WithTokenIssuer(&mockTokenIssuer{err: errors.New("key not found")})

	resp := do(t, ts, http.MethodPost, "/v1/auth/token", map[string]string{"api_key": "bad-key"}, "")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", resp.StatusCode)
	}
}

func TestHandleIssueToken_ValidKey(t *testing.T) {
	srv, _, ts := newTestServer(t)
	srv.WithTokenIssuer(&mockTokenIssuer{token: "jwt.signed.token"})

	resp := do(t, ts, http.MethodPost, "/v1/auth/token", map[string]string{"api_key": "valid-key"}, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}

	var result IssueTokenResponse
	decodeJSON(t, resp, &result)
	if result.Token != "jwt.signed.token" {
		t.Errorf("want token 'jwt.signed.token', got %q", result.Token)
	}
	if result.ExpiresIn != 86400 {
		t.Errorf("want expires_in 86400, got %d", result.ExpiresIn)
	}
}
