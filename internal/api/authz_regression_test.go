package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dnakitare/aether/internal/auth"
)

// TestRequirePermissionDeniesWhenClaimsMissing guards the fail-open hole: with
// authentication enabled, a request that reaches requirePermission without
// claims must be rejected, not allowed through.
func TestRequirePermissionDeniesWhenClaimsMissing(t *testing.T) {
	called := false
	next := func(w http.ResponseWriter, r *http.Request) { called = true; w.WriteHeader(http.StatusOK) }

	t.Run("auth enabled, no claims -> 401", func(t *testing.T) {
		called = false
		srv := &Server{config: Config{EnableAuth: true}}
		h := srv.requirePermission(auth.PermissionAgentRead, next)

		rr := httptest.NewRecorder()
		h(rr, httptest.NewRequest(http.MethodGet, "/v1/agents", nil))

		if rr.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", rr.Code)
		}
		if called {
			t.Error("next handler ran despite missing claims with auth enabled")
		}
	})

	t.Run("auth disabled, no claims -> allowed (dev mode)", func(t *testing.T) {
		called = false
		srv := &Server{config: Config{EnableAuth: false}}
		h := srv.requirePermission(auth.PermissionAgentRead, next)

		rr := httptest.NewRecorder()
		h(rr, httptest.NewRequest(http.MethodGet, "/v1/agents", nil))

		if !called {
			t.Error("next handler should run in dev mode")
		}
	})

	t.Run("claims present but lacking permission -> 403", func(t *testing.T) {
		called = false
		srv := &Server{config: Config{EnableAuth: true}}
		h := srv.requirePermission(auth.PermissionQuotaWrite, next)

		req := httptest.NewRequest(http.MethodPut, "/v1/quotas/t1", nil)
		req = req.WithContext(auth.WithClaims(req.Context(), &auth.Claims{Role: string(auth.RoleAdmin)}))
		rr := httptest.NewRecorder()
		h(rr, req)

		if rr.Code != http.StatusForbidden {
			t.Errorf("status = %d, want 403 (tenant admin lacks quota:write)", rr.Code)
		}
		if called {
			t.Error("next handler ran despite insufficient permission")
		}
	})
}

// TestSchedulerNodesRequiresPlatformAdmin guards S3: cluster topology must not be
// reachable by ordinary tenant roles. A developer is rejected at the route's
// permission gate before the handler runs.
func TestSchedulerNodesRequiresPlatformAdmin(t *testing.T) {
	_, _, ts := newTestServer(t)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/v1/scheduler/nodes", nil)
	req.Header.Set("X-Test-Tenant", "tenant-1")
	req.Header.Set("X-Test-Role", string(auth.RoleDeveloper))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("developer GET /scheduler/nodes = %d, want 403", resp.StatusCode)
	}
}
