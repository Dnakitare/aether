package auth_test

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/aether-runtime/aether/internal/auth"
	"github.com/aether-runtime/aether/pkg/api"
)

func TestAPIKeyManager(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	km := auth.NewAPIKeyManager(logger)
	ctx := context.Background()

	// Create API key
	scopes := []auth.Permission{
		auth.PermissionAgentCreate,
		auth.PermissionAgentRead,
	}

	key, apiKey, err := km.CreateKey(ctx, api.TenantID("tenant-1"), "Test Key", scopes, 24*time.Hour)
	if err != nil {
		t.Fatalf("Failed to create API key: %v", err)
	}

	if key == "" {
		t.Error("Expected non-empty key")
	}

	if apiKey.ID == "" {
		t.Error("Expected API key ID to be set")
	}

	if apiKey.KeyHash == "" {
		t.Error("Expected key hash to be set")
	}

	// Validate key
	validated, err := km.ValidateKey(ctx, key)
	if err != nil {
		t.Fatalf("Failed to validate key: %v", err)
	}

	if validated.ID != apiKey.ID {
		t.Errorf("Validated key ID = %s, want %s", validated.ID, apiKey.ID)
	}

	// List keys
	keys := km.ListKeys(api.TenantID("tenant-1"))
	if len(keys) != 1 {
		t.Errorf("ListKeys returned %d keys, want 1", len(keys))
	}

	// Revoke key
	if err := km.RevokeKey(ctx, api.TenantID("tenant-1"), apiKey.ID); err != nil {
		t.Fatalf("Failed to revoke key: %v", err)
	}

	// Validate revoked key (should fail)
	_, err = km.ValidateKey(ctx, key)
	if err == nil {
		t.Error("Expected error for revoked key")
	}
}

func TestAPIKeyExpiry(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	km := auth.NewAPIKeyManager(logger)
	ctx := context.Background()

	// Create key with short TTL
	key, _, err := km.CreateKey(ctx, api.TenantID("tenant-1"), "Short TTL Key", nil, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("Failed to create key: %v", err)
	}

	// Wait for expiry
	time.Sleep(150 * time.Millisecond)

	// Validate expired key (should fail)
	_, err = km.ValidateKey(ctx, key)
	if err == nil {
		t.Error("Expected error for expired key")
	}
}

func TestAPIKeyRotation(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	km := auth.NewAPIKeyManager(logger)
	ctx := context.Background()

	// Create original key
	oldKey, oldAPIKey, err := km.CreateKey(ctx, api.TenantID("tenant-1"), "Original Key", []auth.Permission{auth.PermissionAgentRead}, 1*time.Hour)
	if err != nil {
		t.Fatalf("Failed to create original key: %v", err)
	}

	// Rotate key
	newKey, newAPIKey, err := km.RotateKey(ctx, api.TenantID("tenant-1"), oldAPIKey.ID, 1*time.Hour)
	if err != nil {
		t.Fatalf("Failed to rotate key: %v", err)
	}

	// New key should validate
	_, err = km.ValidateKey(ctx, newKey)
	if err != nil {
		t.Errorf("Failed to validate new key: %v", err)
	}

	// Old key should not validate (revoked)
	_, err = km.ValidateKey(ctx, oldKey)
	if err == nil {
		t.Error("Expected error for revoked old key")
	}

	// New key should have same scopes
	if len(newAPIKey.Scopes) != len(oldAPIKey.Scopes) {
		t.Errorf("New key scopes = %d, want %d", len(newAPIKey.Scopes), len(oldAPIKey.Scopes))
	}
}
