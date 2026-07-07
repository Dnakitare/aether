package auth_test

import (
	"testing"
	"time"

	"github.com/dnakitare/aether/internal/auth"
	"github.com/dnakitare/aether/pkg/api"
)

func TestJWTManager(t *testing.T) {
	config := auth.Config{
		SecretKey:     "test-secret-key-that-is-at-least-32-chars-long",
		TokenDuration: 1 * time.Hour,
		Issuer:        "aether-test",
	}

	manager, err := auth.NewJWTManager(config)
	if err != nil {
		t.Fatalf("Failed to create JWT manager: %v", err)
	}

	// Generate token
	token, err := manager.GenerateToken(api.TenantID("tenant-1"), "user-1", "developer")
	if err != nil {
		t.Fatalf("Failed to generate token: %v", err)
	}

	if token == "" {
		t.Fatal("Generated token is empty")
	}

	// Validate token
	claims, err := manager.ValidateToken(token)
	if err != nil {
		t.Fatalf("Failed to validate token: %v", err)
	}

	if claims.TenantID != "tenant-1" {
		t.Errorf("TenantID = %s, want tenant-1", claims.TenantID)
	}

	if claims.UserID != "user-1" {
		t.Errorf("UserID = %s, want user-1", claims.UserID)
	}

	if claims.Role != "developer" {
		t.Errorf("Role = %s, want developer", claims.Role)
	}
}

func TestInvalidToken(t *testing.T) {
	config := auth.Config{
		SecretKey:     "test-secret-key-that-is-at-least-32-chars-long",
		TokenDuration: 1 * time.Hour,
		Issuer:        "aether-test",
	}

	manager, err := auth.NewJWTManager(config)
	if err != nil {
		t.Fatalf("Failed to create JWT manager: %v", err)
	}

	// Try to validate invalid token
	_, err = manager.ValidateToken("invalid-token")
	if err == nil {
		t.Error("Expected error for invalid token but got none")
	}
}

func TestRBAC(t *testing.T) {
	tests := []struct {
		name       string
		role       auth.Role
		permission auth.Permission
		want       bool
	}{
		{
			name:       "admin can create agents",
			role:       auth.RoleAdmin,
			permission: auth.PermissionAgentCreate,
			want:       true,
		},
		{
			name:       "developer can create agents",
			role:       auth.RoleDeveloper,
			permission: auth.PermissionAgentCreate,
			want:       true,
		},
		{
			name:       "viewer cannot create agents",
			role:       auth.RoleViewer,
			permission: auth.PermissionAgentCreate,
			want:       false,
		},
		{
			// Quota writes are platform-only: a tenant admin must not be able
			// to raise its own quota or set another tenant's.
			name:       "tenant admin cannot write quotas",
			role:       auth.RoleAdmin,
			permission: auth.PermissionQuotaWrite,
			want:       false,
		},
		{
			name:       "platform admin can write quotas",
			role:       auth.RolePlatformAdmin,
			permission: auth.PermissionQuotaWrite,
			want:       true,
		},
		{
			name:       "developer cannot write quotas",
			role:       auth.RoleDeveloper,
			permission: auth.PermissionQuotaWrite,
			want:       false,
		},
		{
			name:       "viewer can read quotas",
			role:       auth.RoleViewer,
			permission: auth.PermissionQuotaRead,
			want:       true,
		},
		{
			// Cluster topology is platform-level; tenant roles must not see it.
			name:       "tenant admin cannot read scheduler",
			role:       auth.RoleAdmin,
			permission: auth.PermissionSchedulerRead,
			want:       false,
		},
		{
			name:       "platform admin can read scheduler",
			role:       auth.RolePlatformAdmin,
			permission: auth.PermissionSchedulerRead,
			want:       true,
		},
		{
			name:       "only platform admin holds platform permission",
			role:       auth.RoleAdmin,
			permission: auth.PermissionPlatformAdmin,
			want:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := auth.HasPermission(tt.role, tt.permission)
			if got != tt.want {
				t.Errorf("HasPermission() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCheckPermission(t *testing.T) {
	claims := &auth.Claims{
		TenantID: api.TenantID("tenant-1"),
		UserID:   "user-1",
		Role:     "developer",
	}

	// Should have permission
	err := auth.CheckPermission(claims, auth.PermissionAgentCreate)
	if err != nil {
		t.Errorf("CheckPermission failed: %v", err)
	}

	// Should not have permission
	err = auth.CheckPermission(claims, auth.PermissionQuotaWrite)
	if err == nil {
		t.Error("Expected permission denied but got none")
	}
}

func TestIsAdmin(t *testing.T) {
	adminClaims := &auth.Claims{
		Role: "admin",
	}

	if !auth.IsAdmin(adminClaims) {
		t.Error("Expected IsAdmin to return true for admin claims")
	}

	devClaims := &auth.Claims{
		Role: "developer",
	}

	if auth.IsAdmin(devClaims) {
		t.Error("Expected IsAdmin to return false for developer claims")
	}
}
