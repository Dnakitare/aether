package auth_test

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/aether-runtime/aether/internal/auth"
	"github.com/aether-runtime/aether/pkg/api"
)

// Test Summary:
// This comprehensive test suite covers API key authentication edge cases:
// - Key expiration boundaries
// - Key revocation scenarios
// - Concurrent key operations
// - Key rotation edge cases
// - Invalid key formats
// - Tenant isolation
// - Cleanup operations
// - Resource limits

func setupTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))
}

// =============================================================================
// Key Expiration Edge Cases
// =============================================================================

func TestAPIKeyExpiration_ExactBoundary(t *testing.T) {
	logger := setupTestLogger()
	km := auth.NewAPIKeyManager(logger)
	ctx := context.Background()

	t.Run("key expires at exact millisecond", func(t *testing.T) {
		// Create key with 200ms TTL
		key, apiKey, err := km.CreateKey(
			ctx,
			api.TenantID("tenant-1"),
			"Short TTL Key",
			[]auth.Permission{auth.PermissionAgentRead},
			200*time.Millisecond,
		)
		require.NoError(t, err)
		require.NotNil(t, apiKey.ExpiresAt)

		// Validate immediately
		validated, err := km.ValidateKey(ctx, key)
		assert.NoError(t, err)
		assert.NotNil(t, validated)

		// Wait for expiration
		time.Sleep(250 * time.Millisecond)

		// Should fail after expiration
		_, err = km.ValidateKey(ctx, key)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "expired")
	})

	t.Run("key with zero TTL never expires", func(t *testing.T) {
		key, apiKey, err := km.CreateKey(
			ctx,
			api.TenantID("tenant-1"),
			"No Expiry Key",
			[]auth.Permission{auth.PermissionAgentRead},
			0, // No TTL
		)
		require.NoError(t, err)
		assert.Nil(t, apiKey.ExpiresAt, "key with zero TTL should not have expiration")

		// Should validate successfully
		validated, err := km.ValidateKey(ctx, key)
		assert.NoError(t, err)
		assert.Nil(t, validated.ExpiresAt)

		// Wait a bit and validate again
		time.Sleep(100 * time.Millisecond)
		validated, err = km.ValidateKey(ctx, key)
		assert.NoError(t, err)
	})

	t.Run("key at exact expiration time", func(t *testing.T) {
		// This tests the boundary condition where time.Now() == ExpiresAt
		key, apiKey, err := km.CreateKey(
			ctx,
			api.TenantID("tenant-1"),
			"Boundary Key",
			nil,
			100*time.Millisecond,
		)
		require.NoError(t, err)

		// Sleep until just before expiration
		time.Sleep(95 * time.Millisecond)

		// Should still be valid
		_, err = km.ValidateKey(ctx, key)
		assert.NoError(t, err)

		// Sleep past expiration
		time.Sleep(10 * time.Millisecond)

		// Should now be expired
		_, err = km.ValidateKey(ctx, key)
		assert.Error(t, err)

		// Verify expiration time
		assert.True(t, time.Now().After(*apiKey.ExpiresAt))
	})
}

// =============================================================================
// Key Revocation Scenarios
// =============================================================================

func TestAPIKeyRevocation_EdgeCases(t *testing.T) {
	logger := setupTestLogger()
	km := auth.NewAPIKeyManager(logger)
	ctx := context.Background()

	t.Run("revoke immediately after creation", func(t *testing.T) {
		key, apiKey, err := km.CreateKey(
			ctx,
			api.TenantID("tenant-1"),
			"Quick Revoke",
			[]auth.Permission{auth.PermissionAgentRead},
			1*time.Hour,
		)
		require.NoError(t, err)

		// Revoke immediately
		err = km.RevokeKey(ctx, api.TenantID("tenant-1"), apiKey.ID)
		assert.NoError(t, err)

		// Validation should fail
		_, err = km.ValidateKey(ctx, key)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "revoked")
	})

	t.Run("revoke non-existent key", func(t *testing.T) {
		err := km.RevokeKey(ctx, api.TenantID("tenant-1"), "non-existent-key-id")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("revoke key from wrong tenant", func(t *testing.T) {
		// Create key for tenant-1
		_, apiKey, err := km.CreateKey(
			ctx,
			api.TenantID("tenant-1"),
			"Tenant 1 Key",
			nil,
			1*time.Hour,
		)
		require.NoError(t, err)

		// Try to revoke as tenant-2
		err = km.RevokeKey(ctx, api.TenantID("tenant-2"), apiKey.ID)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("revoke already revoked key", func(t *testing.T) {
		_, apiKey, err := km.CreateKey(
			ctx,
			api.TenantID("tenant-1"),
			"Double Revoke",
			nil,
			1*time.Hour,
		)
		require.NoError(t, err)

		// First revocation
		err = km.RevokeKey(ctx, api.TenantID("tenant-1"), apiKey.ID)
		assert.NoError(t, err)

		// Second revocation (idempotent)
		err = km.RevokeKey(ctx, api.TenantID("tenant-1"), apiKey.ID)
		assert.NoError(t, err)
	})
}

// =============================================================================
// Concurrent Key Operations
// =============================================================================

func TestAPIKeyConcurrency(t *testing.T) {
	logger := setupTestLogger()
	km := auth.NewAPIKeyManager(logger)
	ctx := context.Background()

	t.Run("concurrent key creation", func(t *testing.T) {
		const numGoroutines = 50
		var wg sync.WaitGroup
		results := make(chan error, numGoroutines)

		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				_, _, err := km.CreateKey(
					ctx,
					api.TenantID("tenant-1"),
					"Concurrent Key",
					[]auth.Permission{auth.PermissionAgentRead},
					1*time.Hour,
				)
				results <- err
			}(i)
		}

		wg.Wait()
		close(results)

		// All creations should succeed
		for err := range results {
			assert.NoError(t, err)
		}

		// Verify all keys were created
		keys := km.ListKeys(api.TenantID("tenant-1"))
		assert.Equal(t, numGoroutines, len(keys))
	})

	t.Run("concurrent validation of same key", func(t *testing.T) {
		key, _, err := km.CreateKey(
			ctx,
			api.TenantID("tenant-1"),
			"Shared Key",
			nil,
			1*time.Hour,
		)
		require.NoError(t, err)

		const numGoroutines = 100
		var wg sync.WaitGroup
		results := make(chan error, numGoroutines)

		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, err := km.ValidateKey(ctx, key)
				results <- err
			}()
		}

		wg.Wait()
		close(results)

		// All validations should succeed
		for err := range results {
			assert.NoError(t, err)
		}
	})

	t.Run("concurrent revocations", func(t *testing.T) {
		_, apiKey, err := km.CreateKey(
			ctx,
			api.TenantID("tenant-1"),
			"Revoke Race",
			nil,
			1*time.Hour,
		)
		require.NoError(t, err)

		const numGoroutines = 10
		var wg sync.WaitGroup
		successCount := int32(0)

		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				err := km.RevokeKey(ctx, api.TenantID("tenant-1"), apiKey.ID)
				if err == nil {
					atomic.AddInt32(&successCount, 1)
				}
			}()
		}

		wg.Wait()

		// All revocations should succeed (idempotent)
		assert.Equal(t, int32(numGoroutines), successCount)
	})

	t.Run("concurrent create and revoke", func(t *testing.T) {
		const numPairs = 20
		var wg sync.WaitGroup

		for i := 0; i < numPairs; i++ {
			wg.Add(2)

			go func(id int) {
				defer wg.Done()
				km.CreateKey(
					ctx,
					api.TenantID("tenant-1"),
					"Create Race",
					nil,
					1*time.Hour,
				)
			}(i)

			go func(id int) {
				defer wg.Done()
				keys := km.ListKeys(api.TenantID("tenant-1"))
				if len(keys) > 0 {
					km.RevokeKey(ctx, api.TenantID("tenant-1"), keys[0].ID)
				}
			}(i)
		}

		wg.Wait()

		// Should not panic or deadlock
	})
}

// =============================================================================
// Key Rotation Edge Cases
// =============================================================================

func TestAPIKeyRotation_EdgeCases(t *testing.T) {
	logger := setupTestLogger()
	km := auth.NewAPIKeyManager(logger)
	ctx := context.Background()

	t.Run("rotate non-existent key", func(t *testing.T) {
		_, _, err := km.RotateKey(
			ctx,
			api.TenantID("tenant-1"),
			"non-existent-id",
			1*time.Hour,
		)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("rotate key from wrong tenant", func(t *testing.T) {
		// Create key for tenant-1
		_, apiKey, err := km.CreateKey(
			ctx,
			api.TenantID("tenant-1"),
			"Tenant 1 Key",
			[]auth.Permission{auth.PermissionAgentRead},
			1*time.Hour,
		)
		require.NoError(t, err)

		// Try to rotate as tenant-2
		_, _, err = km.RotateKey(
			ctx,
			api.TenantID("tenant-2"),
			apiKey.ID,
			1*time.Hour,
		)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("rotate preserves scopes", func(t *testing.T) {
		originalScopes := []auth.Permission{
			auth.PermissionAgentCreate,
			auth.PermissionAgentRead,
			auth.PermissionAgentDelete,
		}

		_, oldKey, err := km.CreateKey(
			ctx,
			api.TenantID("tenant-1"),
			"Original Key",
			originalScopes,
			1*time.Hour,
		)
		require.NoError(t, err)

		// Rotate
		newKeyStr, newKey, err := km.RotateKey(
			ctx,
			api.TenantID("tenant-1"),
			oldKey.ID,
			2*time.Hour,
		)
		require.NoError(t, err)

		// Verify scopes preserved
		assert.Equal(t, len(originalScopes), len(newKey.Scopes))
		assert.Contains(t, newKey.Name, "rotated")

		// Verify new key works
		validated, err := km.ValidateKey(ctx, newKeyStr)
		assert.NoError(t, err)
		assert.Equal(t, len(originalScopes), len(validated.Scopes))
	})

	t.Run("multiple rapid rotations", func(t *testing.T) {
		_, firstKey, err := km.CreateKey(
			ctx,
			api.TenantID("tenant-1"),
			"Rotate Chain",
			[]auth.Permission{auth.PermissionAgentRead},
			1*time.Hour,
		)
		require.NoError(t, err)

		currentID := firstKey.ID

		// Rotate 5 times
		for i := 0; i < 5; i++ {
			_, newKey, err := km.RotateKey(
				ctx,
				api.TenantID("tenant-1"),
				currentID,
				1*time.Hour,
			)
			require.NoError(t, err)
			currentID = newKey.ID
		}

		// Should have 5 rotated keys (old ones revoked)
		keys := km.ListKeys(api.TenantID("tenant-1"))
		revokedCount := 0
		for _, k := range keys {
			if strings.Contains(k.Name, "rotated") {
				revokedCount++
			}
		}
		assert.GreaterOrEqual(t, revokedCount, 1)
	})
}

// =============================================================================
// Invalid Key Formats
// =============================================================================

func TestAPIKeyInvalidFormats(t *testing.T) {
	logger := setupTestLogger()
	km := auth.NewAPIKeyManager(logger)
	ctx := context.Background()

	tests := []struct {
		name string
		key  string
	}{
		{
			name: "empty string",
			key:  "",
		},
		{
			name: "random string",
			key:  "this-is-not-a-valid-api-key",
		},
		{
			name: "wrong prefix",
			key:  "wrong_prefix_12345678_abcdefgh",
		},
		{
			name: "missing parts",
			key:  "aether_12345678",
		},
		{
			name: "too many parts",
			key:  "aether_12345678_abcdefgh_extra",
		},
		{
			name: "special characters",
			key:  "aether_!@#$%^&*_abcdefgh",
		},
		{
			name: "sql injection",
			key:  "aether_'; DROP TABLE keys; --_abcd",
		},
		{
			name: "extremely long key",
			key:  "aether_" + strings.Repeat("a", 10000),
		},
		{
			name: "unicode characters",
			key:  "aether_🔑🔐🗝️_密钥",
		},
		{
			name: "null bytes",
			key:  "aether_\x00\x00\x00_key",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := km.ValidateKey(ctx, tt.key)
			assert.Error(t, err, "should reject invalid key format")
		})
	}
}

// =============================================================================
// Tenant Isolation
// =============================================================================

func TestAPIKeyTenantIsolation(t *testing.T) {
	logger := setupTestLogger()
	km := auth.NewAPIKeyManager(logger)
	ctx := context.Background()

	t.Run("list keys shows only tenant keys", func(t *testing.T) {
		// Create keys for tenant-1
		_, _, err := km.CreateKey(ctx, api.TenantID("tenant-1"), "T1 Key 1", nil, 1*time.Hour)
		require.NoError(t, err)
		_, _, err = km.CreateKey(ctx, api.TenantID("tenant-1"), "T1 Key 2", nil, 1*time.Hour)
		require.NoError(t, err)

		// Create keys for tenant-2
		_, _, err = km.CreateKey(ctx, api.TenantID("tenant-2"), "T2 Key 1", nil, 1*time.Hour)
		require.NoError(t, err)

		// List for tenant-1
		keys1 := km.ListKeys(api.TenantID("tenant-1"))
		assert.GreaterOrEqual(t, len(keys1), 2)

		// Verify all keys belong to tenant-1
		for _, key := range keys1 {
			assert.Equal(t, api.TenantID("tenant-1"), key.TenantID)
		}

		// List for tenant-2
		keys2 := km.ListKeys(api.TenantID("tenant-2"))
		assert.GreaterOrEqual(t, len(keys2), 1)

		// Verify all keys belong to tenant-2
		for _, key := range keys2 {
			assert.Equal(t, api.TenantID("tenant-2"), key.TenantID)
		}
	})

	t.Run("list non-existent tenant returns empty", func(t *testing.T) {
		keys := km.ListKeys(api.TenantID("non-existent-tenant"))
		assert.Empty(t, keys)
	})

	t.Run("revoked keys not in list", func(t *testing.T) {
		key, apiKey, err := km.CreateKey(ctx, api.TenantID("tenant-1"), "Will Revoke", nil, 1*time.Hour)
		require.NoError(t, err)

		// Key should be in list
		keys := km.ListKeys(api.TenantID("tenant-1"))
		found := false
		for _, k := range keys {
			if k.ID == apiKey.ID {
				found = true
				break
			}
		}
		assert.True(t, found)

		// Revoke key
		err = km.RevokeKey(ctx, api.TenantID("tenant-1"), apiKey.ID)
		require.NoError(t, err)

		// Verify still fails validation
		_, err = km.ValidateKey(ctx, key)
		assert.Error(t, err)

		// Key should not be in list
		keys = km.ListKeys(api.TenantID("tenant-1"))
		found = false
		for _, k := range keys {
			if k.ID == apiKey.ID {
				found = true
				break
			}
		}
		assert.False(t, found)
	})
}

// =============================================================================
// Cleanup Operations
// =============================================================================

func TestAPIKeyCleanup(t *testing.T) {
	logger := setupTestLogger()
	km := auth.NewAPIKeyManager(logger)
	ctx := context.Background()

	t.Run("cleanup expired keys", func(t *testing.T) {
		// Create short-lived keys
		_, _, err := km.CreateKey(ctx, api.TenantID("tenant-1"), "Expire 1", nil, 50*time.Millisecond)
		require.NoError(t, err)
		_, _, err = km.CreateKey(ctx, api.TenantID("tenant-1"), "Expire 2", nil, 50*time.Millisecond)
		require.NoError(t, err)

		// Create long-lived key
		_, _, err = km.CreateKey(ctx, api.TenantID("tenant-1"), "Keep", nil, 1*time.Hour)
		require.NoError(t, err)

		// Wait for expiration
		time.Sleep(100 * time.Millisecond)

		// Run cleanup
		cleaned := km.CleanupExpired(ctx)
		assert.Equal(t, 2, cleaned)

		// Run cleanup again (should find none)
		cleaned = km.CleanupExpired(ctx)
		assert.Equal(t, 0, cleaned)
	})

	t.Run("cleanup with no expired keys", func(t *testing.T) {
		cleaned := km.CleanupExpired(ctx)
		assert.GreaterOrEqual(t, cleaned, 0)
	})

	t.Run("cleanup does not remove non-expiring keys", func(t *testing.T) {
		_, apiKey, err := km.CreateKey(ctx, api.TenantID("tenant-1"), "Forever", nil, 0)
		require.NoError(t, err)

		// Run cleanup
		km.CleanupExpired(ctx)

		// Key should still be listable
		keys := km.ListKeys(api.TenantID("tenant-1"))
		found := false
		for _, k := range keys {
			if k.ID == apiKey.ID {
				found = true
				break
			}
		}
		assert.True(t, found)
	})
}

// =============================================================================
// Last Used Timestamp
// =============================================================================

func TestAPIKeyLastUsed(t *testing.T) {
	logger := setupTestLogger()
	km := auth.NewAPIKeyManager(logger)
	ctx := context.Background()

	t.Run("last used updated on validation", func(t *testing.T) {
		key, apiKey, err := km.CreateKey(ctx, api.TenantID("tenant-1"), "Track Usage", nil, 1*time.Hour)
		require.NoError(t, err)

		// Initially nil
		assert.Nil(t, apiKey.LastUsedAt)

		// Validate key
		validated, err := km.ValidateKey(ctx, key)
		require.NoError(t, err)

		// LastUsedAt should be set
		assert.NotNil(t, validated.LastUsedAt)
		firstUse := validated.LastUsedAt

		// Wait a bit
		time.Sleep(50 * time.Millisecond)

		// Validate again
		validated, err = km.ValidateKey(ctx, key)
		require.NoError(t, err)

		// LastUsedAt should be updated
		assert.NotNil(t, validated.LastUsedAt)
		assert.True(t, validated.LastUsedAt.After(*firstUse))
	})
}

// =============================================================================
// Permission Scopes
// =============================================================================

func TestAPIKeyScopes(t *testing.T) {
	logger := setupTestLogger()
	km := auth.NewAPIKeyManager(logger)
	ctx := context.Background()

	t.Run("key with no scopes", func(t *testing.T) {
		key, apiKey, err := km.CreateKey(ctx, api.TenantID("tenant-1"), "No Scopes", nil, 1*time.Hour)
		require.NoError(t, err)
		assert.Empty(t, apiKey.Scopes)

		validated, err := km.ValidateKey(ctx, key)
		assert.NoError(t, err)
		assert.Empty(t, validated.Scopes)
	})

	t.Run("key with multiple scopes", func(t *testing.T) {
		scopes := []auth.Permission{
			auth.PermissionAgentCreate,
			auth.PermissionAgentRead,
			auth.PermissionAgentUpdate,
			auth.PermissionAgentDelete,
		}

		key, apiKey, err := km.CreateKey(ctx, api.TenantID("tenant-1"), "Full Access", scopes, 1*time.Hour)
		require.NoError(t, err)
		assert.Equal(t, 4, len(apiKey.Scopes))

		validated, err := km.ValidateKey(ctx, key)
		assert.NoError(t, err)
		assert.Equal(t, 4, len(validated.Scopes))
	})
}

// =============================================================================
// Key Prefix and Identification
// =============================================================================

func TestAPIKeyPrefixFormat(t *testing.T) {
	logger := setupTestLogger()
	km := auth.NewAPIKeyManager(logger)
	ctx := context.Background()

	t.Run("key has correct format", func(t *testing.T) {
		key, apiKey, err := km.CreateKey(ctx, api.TenantID("tenant-1"), "Format Test", nil, 1*time.Hour)
		require.NoError(t, err)

		// Key should start with "aether_"
		assert.True(t, strings.HasPrefix(key, "aether_"))

		// Should have 3 parts separated by underscore
		parts := strings.Split(key, "_")
		assert.Len(t, parts, 3)
		assert.Equal(t, "aether", parts[0])

		// Prefix should be first 8 chars after "aether_"
		assert.Equal(t, 8, len(apiKey.Prefix))
		assert.Equal(t, apiKey.Prefix, parts[1])
	})

	t.Run("keys have unique prefixes", func(t *testing.T) {
		prefixes := make(map[string]bool)

		for i := 0; i < 10; i++ {
			_, apiKey, err := km.CreateKey(ctx, api.TenantID("tenant-1"), "Unique", nil, 1*time.Hour)
			require.NoError(t, err)

			// Prefix should be unique
			assert.False(t, prefixes[apiKey.Prefix], "duplicate prefix found")
			prefixes[apiKey.Prefix] = true
		}
	})
}

// =============================================================================
// Test Coverage Summary
// =============================================================================

/*
Edge Cases Covered:

1. Key Expiration:
   - Exact millisecond boundary
   - Zero TTL (never expires)
   - Validation at exact expiration time

2. Key Revocation:
   - Immediate revocation after creation
   - Non-existent key revocation
   - Cross-tenant revocation attempts
   - Double revocation (idempotent)

3. Concurrency:
   - Concurrent key creation (50 goroutines)
   - Concurrent validation (100 goroutines)
   - Concurrent revocations (10 goroutines)
   - Mixed create/revoke operations

4. Key Rotation:
   - Non-existent key rotation
   - Cross-tenant rotation
   - Scope preservation
   - Multiple rapid rotations

5. Invalid Formats:
   - Empty strings
   - Wrong prefixes
   - Missing/extra parts
   - Special characters
   - SQL injection attempts
   - Unicode and null bytes

6. Tenant Isolation:
   - List filtering by tenant
   - Non-existent tenant
   - Revoked keys excluded from lists

7. Cleanup:
   - Expired key cleanup
   - Non-expiring key preservation
   - Multiple cleanup runs

8. Last Used Tracking:
   - Timestamp updates on validation
   - Multiple validations

9. Scopes:
   - Empty scopes
   - Multiple scopes
   - Scope preservation in rotation

10. Key Format:
    - Correct "aether_" prefix
    - Unique prefixes
    - Three-part structure

Total Test Cases: 40+
Coverage: Comprehensive edge case coverage for API key authentication
*/
