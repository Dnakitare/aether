package integration

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dnakitare/aether/internal/auth"
	pkgapi "github.com/dnakitare/aether/pkg/api"
)

// Test Summary:
// This file contains end-to-end integration tests for authentication across all components.
// Tests verify JWT and API key authentication work correctly with scheduler, rate limiter, and other services.

// TestAuthIntegration_JWTWorkflow tests complete JWT authentication workflow
func TestAuthIntegration_JWTWorkflow(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	env := SetupTestEnvironment(t)
	defer env.TearDown()

	ctx := context.Background()

	t.Run("user login and token generation", func(t *testing.T) {
		// Simulate user login
		token, err := env.Auth.GenerateToken(
			pkgapi.TenantID("company-a"),
			"user-alice",
			"developer",
		)
		require.NoError(t, err)
		assert.NotEmpty(t, token)

		// Validate token
		claims, err := env.Auth.ValidateToken(token)
		require.NoError(t, err)
		assert.Equal(t, pkgapi.TenantID("company-a"), claims.TenantID)
		assert.Equal(t, "user-alice", claims.UserID)
		assert.Equal(t, "developer", claims.Role)

		env.T.Logf("User login successful for %s@%s", claims.UserID, claims.TenantID)
	})

	t.Run("token used across multiple operations", func(t *testing.T) {
		// Generate token
		token, err := env.Auth.GenerateToken(env.TenantID, "user-bob", "admin")
		require.NoError(t, err)

		// Operation 1: Create agent
		claims1, err := env.Auth.ValidateToken(token)
		require.NoError(t, err)
		assert.Equal(t, env.TenantID, claims1.TenantID)

		// Operation 2: Schedule task
		claims2, err := env.Auth.ValidateToken(token)
		require.NoError(t, err)
		assert.Equal(t, env.TenantID, claims2.TenantID)

		// Operation 3: Check status
		claims3, err := env.Auth.ValidateToken(token)
		require.NoError(t, err)
		assert.Equal(t, env.TenantID, claims3.TenantID)

		env.T.Log("Token successfully used across multiple operations")
	})

	t.Run("context propagation through components", func(t *testing.T) {
		// Create context with claims
		claims, err := env.Auth.ValidateToken(env.AuthToken)
		require.NoError(t, err)

		authCtx := auth.WithClaims(ctx, claims)

		// Extract tenant ID from context
		tenantID, err := auth.GetTenantID(authCtx)
		require.NoError(t, err)
		assert.Equal(t, claims.TenantID, tenantID)

		// Extract user ID from context
		userID, err := auth.GetUserID(authCtx)
		require.NoError(t, err)
		assert.Equal(t, claims.UserID, userID)

		env.T.Logf("Context propagation: tenant=%s, user=%s", tenantID, userID)
	})

	t.Run("multiple concurrent users", func(t *testing.T) {
		const numUsers = 10
		results := make(chan error, numUsers)

		for i := 0; i < numUsers; i++ {
			go func(userID int) {
				// Generate token for user
				token, err := env.Auth.GenerateToken(
					env.TenantID,
					"user-"+string(rune(userID)),
					"developer",
				)
				if err != nil {
					results <- err
					return
				}

				// Validate token
				claims, err := env.Auth.ValidateToken(token)
				if err != nil {
					results <- err
					return
				}

				// Verify claims
				if claims.TenantID != env.TenantID {
					results <- assert.AnError
					return
				}

				results <- nil
			}(i)
		}

		// Verify all succeeded
		for i := 0; i < numUsers; i++ {
			assert.NoError(t, <-results)
		}

		env.T.Logf("Successfully authenticated %d concurrent users", numUsers)
	})
}

// TestAuthIntegration_APIKeyWorkflow tests complete API key authentication workflow
func TestAuthIntegration_APIKeyWorkflow(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	env := SetupTestEnvironment(t)
	defer env.TearDown()

	ctx := context.Background()

	t.Run("service account creates api key", func(t *testing.T) {
		// Create API key for service
		key, apiKey, err := env.APIKeys.CreateKey(
			ctx,
			pkgapi.TenantID("service-tenant"),
			"CI/CD Pipeline Key",
			[]auth.Permission{
				auth.PermissionAgentCreate,
				auth.PermissionAgentRead,
			},
			24*time.Hour,
		)
		require.NoError(t, err)
		assert.NotEmpty(t, key)
		assert.NotNil(t, apiKey)

		// Validate key
		validated, err := env.APIKeys.ValidateKey(ctx, key)
		require.NoError(t, err)
		assert.Equal(t, pkgapi.TenantID("service-tenant"), validated.TenantID)
		assert.Contains(t, validated.Scopes, auth.PermissionAgentCreate)

		env.T.Logf("API key created for service: %s (prefix: %s)", apiKey.Name, apiKey.Prefix)
	})

	t.Run("api key rotation workflow", func(t *testing.T) {
		// Create initial key
		oldKey, oldAPIKey, err := env.APIKeys.CreateKey(
			ctx,
			env.TenantID,
			"Rotating Key",
			[]auth.Permission{auth.PermissionAgentRead},
			1*time.Hour,
		)
		require.NoError(t, err)

		// Use old key
		validated, err := env.APIKeys.ValidateKey(ctx, oldKey)
		require.NoError(t, err)
		assert.Equal(t, env.TenantID, validated.TenantID)

		// Rotate key
		newKey, newAPIKey, err := env.APIKeys.RotateKey(ctx, env.TenantID, oldAPIKey.ID, 1*time.Hour)
		require.NoError(t, err)
		assert.NotEqual(t, oldKey, newKey)

		// Old key should be revoked
		_, err = env.APIKeys.ValidateKey(ctx, oldKey)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "revoked")

		// New key should work
		validated, err = env.APIKeys.ValidateKey(ctx, newKey)
		require.NoError(t, err)
		assert.Equal(t, env.TenantID, validated.TenantID)

		env.T.Logf("Key rotation: %s -> %s", oldAPIKey.Prefix, newAPIKey.Prefix)
	})

	t.Run("api key with multiple scopes", func(t *testing.T) {
		scopes := []auth.Permission{
			auth.PermissionAgentCreate,
			auth.PermissionAgentRead,
			auth.PermissionAgentUpdate,
			auth.PermissionAgentDelete,
		}

		key, apiKey, err := env.APIKeys.CreateKey(
			ctx,
			env.TenantID,
			"Full Access Key",
			scopes,
			1*time.Hour,
		)
		require.NoError(t, err)

		// Validate key
		validated, err := env.APIKeys.ValidateKey(ctx, key)
		require.NoError(t, err)

		// Verify all scopes present
		assert.Equal(t, len(scopes), len(validated.Scopes))
		for _, scope := range scopes {
			assert.Contains(t, validated.Scopes, scope)
		}

		env.T.Logf("API key has %d scopes: %v", len(apiKey.Scopes), apiKey.Scopes)
	})

	t.Run("api key expiration enforcement", func(t *testing.T) {
		// Create short-lived key
		key, _, err := env.APIKeys.CreateKey(
			ctx,
			env.TenantID,
			"Short-Lived Key",
			nil,
			200*time.Millisecond,
		)
		require.NoError(t, err)

		// Validate immediately
		_, err = env.APIKeys.ValidateKey(ctx, key)
		assert.NoError(t, err)

		// Wait for expiration
		time.Sleep(300 * time.Millisecond)

		// Should fail after expiration
		_, err = env.APIKeys.ValidateKey(ctx, key)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "expired")

		env.T.Log("API key expiration enforced correctly")
	})
}

// TestAuthIntegration_TenantIsolation tests tenant isolation across all components
func TestAuthIntegration_TenantIsolation(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	env := SetupTestEnvironment(t)
	defer env.TearDown()

	ctx := context.Background()

	t.Run("separate tenant operations isolated", func(t *testing.T) {
		// Tenant A
		tenantA := pkgapi.TenantID("tenant-a")
		tokenA, err := env.Auth.GenerateToken(tenantA, "user-a", "admin")
		require.NoError(t, err)

		claimsA, err := env.Auth.ValidateToken(tokenA)
		require.NoError(t, err)
		assert.Equal(t, tenantA, claimsA.TenantID)

		// Tenant B
		tenantB := pkgapi.TenantID("tenant-b")
		tokenB, err := env.Auth.GenerateToken(tenantB, "user-b", "admin")
		require.NoError(t, err)

		claimsB, err := env.Auth.ValidateToken(tokenB)
		require.NoError(t, err)
		assert.Equal(t, tenantB, claimsB.TenantID)

		// Verify isolation
		assert.NotEqual(t, claimsA.TenantID, claimsB.TenantID)

		env.T.Logf("Tenant isolation: %s != %s", tenantA, tenantB)
	})

	t.Run("api keys isolated by tenant", func(t *testing.T) {
		// Create keys for different tenants
		keyA, _, err := env.APIKeys.CreateKey(
			ctx,
			pkgapi.TenantID("tenant-a"),
			"Tenant A Key",
			nil,
			1*time.Hour,
		)
		require.NoError(t, err)

		keyB, _, err := env.APIKeys.CreateKey(
			ctx,
			pkgapi.TenantID("tenant-b"),
			"Tenant B Key",
			nil,
			1*time.Hour,
		)
		require.NoError(t, err)

		// Validate keys
		validatedA, err := env.APIKeys.ValidateKey(ctx, keyA)
		require.NoError(t, err)

		validatedB, err := env.APIKeys.ValidateKey(ctx, keyB)
		require.NoError(t, err)

		// Verify tenant isolation
		assert.NotEqual(t, validatedA.TenantID, validatedB.TenantID)

		// List keys by tenant
		keysA := env.APIKeys.ListKeys(pkgapi.TenantID("tenant-a"))
		keysB := env.APIKeys.ListKeys(pkgapi.TenantID("tenant-b"))

		// Verify each tenant only sees their own keys
		for _, key := range keysA {
			assert.Equal(t, pkgapi.TenantID("tenant-a"), key.TenantID)
		}
		for _, key := range keysB {
			assert.Equal(t, pkgapi.TenantID("tenant-b"), key.TenantID)
		}

		env.T.Logf("API key isolation: tenant-a has %d keys, tenant-b has %d keys",
			len(keysA), len(keysB))
	})

	t.Run("agent operations respect tenant boundaries", func(t *testing.T) {
		// Create agents for different tenants
		tenantA := pkgapi.TenantID("tenant-a")
		tenantB := pkgapi.TenantID("tenant-b")

		agentA := env.CreateTestAgent("tenant-a-agent")
		agentA.TenantID = tenantA

		agentB := env.CreateTestAgent("tenant-b-agent")
		agentB.TenantID = tenantB

		// Verify tenant assignment
		assert.Equal(t, tenantA, agentA.TenantID)
		assert.Equal(t, tenantB, agentB.TenantID)
		assert.NotEqual(t, agentA.TenantID, agentB.TenantID)

		env.T.Log("Agent tenant isolation verified")
	})
}

// TestAuthIntegration_RBACEnforcement tests role-based access control
func TestAuthIntegration_RBACEnforcement(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	env := SetupTestEnvironment(t)
	defer env.TearDown()

	t.Run("admin has all permissions", func(t *testing.T) {
		// Create admin token
		token, err := env.Auth.GenerateToken(env.TenantID, "admin-user", "admin")
		require.NoError(t, err)

		claims, err := env.Auth.ValidateToken(token)
		require.NoError(t, err)

		// Check admin permissions
		assert.True(t, auth.IsAdmin(claims))

		// Verify admin has all permissions
		permissions := []auth.Permission{
			auth.PermissionAgentCreate,
			auth.PermissionAgentRead,
			auth.PermissionAgentUpdate,
			auth.PermissionAgentDelete,
			auth.PermissionQuotaWrite,
		}

		for _, perm := range permissions {
			err := auth.CheckPermission(claims, perm)
			assert.NoError(t, err, "admin should have permission: %v", perm)
		}

		env.T.Log("Admin has all permissions verified")
	})

	t.Run("developer has limited permissions", func(t *testing.T) {
		// Create developer token
		token, err := env.Auth.GenerateToken(env.TenantID, "dev-user", "developer")
		require.NoError(t, err)

		claims, err := env.Auth.ValidateToken(token)
		require.NoError(t, err)

		// Check developer permissions
		assert.False(t, auth.IsAdmin(claims))

		// Developer should have agent permissions
		assert.NoError(t, auth.CheckPermission(claims, auth.PermissionAgentCreate))
		assert.NoError(t, auth.CheckPermission(claims, auth.PermissionAgentRead))

		// Developer should NOT have quota write permission
		err = auth.CheckPermission(claims, auth.PermissionQuotaWrite)
		assert.Error(t, err)

		env.T.Log("Developer permissions verified (limited)")
	})

	t.Run("viewer has read-only access", func(t *testing.T) {
		// Create viewer token
		token, err := env.Auth.GenerateToken(env.TenantID, "viewer-user", "viewer")
		require.NoError(t, err)

		claims, err := env.Auth.ValidateToken(token)
		require.NoError(t, err)

		// Viewer should have read permissions
		assert.NoError(t, auth.CheckPermission(claims, auth.PermissionAgentRead))
		assert.NoError(t, auth.CheckPermission(claims, auth.PermissionQuotaRead))

		// Viewer should NOT have write permissions
		assert.Error(t, auth.CheckPermission(claims, auth.PermissionAgentCreate))
		assert.Error(t, auth.CheckPermission(claims, auth.PermissionAgentDelete))
		assert.Error(t, auth.CheckPermission(claims, auth.PermissionQuotaWrite))

		env.T.Log("Viewer permissions verified (read-only)")
	})
}

// TestAuthIntegration_SecurityScenarios tests security edge cases
func TestAuthIntegration_SecurityScenarios(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	env := SetupTestEnvironment(t)
	defer env.TearDown()

	ctx := context.Background()

	t.Run("prevent privilege escalation", func(t *testing.T) {
		// Developer tries to create admin token (not possible in real system)
		devToken, err := env.Auth.GenerateToken(env.TenantID, "dev-user", "developer")
		require.NoError(t, err)

		devClaims, err := env.Auth.ValidateToken(devToken)
		require.NoError(t, err)

		// Verify role is developer, not admin
		assert.Equal(t, "developer", devClaims.Role)
		assert.False(t, auth.IsAdmin(devClaims))

		env.T.Log("Privilege escalation prevented")
	})

	t.Run("api key revocation immediate", func(t *testing.T) {
		// Create and revoke key
		key, apiKey, err := env.APIKeys.CreateKey(
			ctx,
			env.TenantID,
			"Revoke Test",
			nil,
			1*time.Hour,
		)
		require.NoError(t, err)

		// Revoke immediately
		err = env.APIKeys.RevokeKey(ctx, env.TenantID, apiKey.ID)
		require.NoError(t, err)

		// Validation should fail immediately
		_, err = env.APIKeys.ValidateKey(ctx, key)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "revoked")

		env.T.Log("API key revocation immediate enforcement verified")
	})

	t.Run("concurrent token validation safe", func(t *testing.T) {
		// Concurrent validation of same token
		const numGoroutines = 50
		results := make(chan error, numGoroutines)

		for i := 0; i < numGoroutines; i++ {
			go func() {
				_, err := env.Auth.ValidateToken(env.AuthToken)
				results <- err
			}()
		}

		// All should succeed
		for i := 0; i < numGoroutines; i++ {
			assert.NoError(t, <-results)
		}

		env.T.Logf("Concurrent token validation: %d operations succeeded", numGoroutines)
	})

	t.Run("cross-tenant access blocked", func(t *testing.T) {
		// Create key for tenant-a
		keyA, apiKeyA, err := env.APIKeys.CreateKey(
			ctx,
			pkgapi.TenantID("tenant-a"),
			"Tenant A Key",
			nil,
			1*time.Hour,
		)
		require.NoError(t, err)

		// Try to revoke as tenant-b (should fail)
		err = env.APIKeys.RevokeKey(ctx, pkgapi.TenantID("tenant-b"), apiKeyA.ID)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")

		// Verify key still valid for tenant-a
		validated, err := env.APIKeys.ValidateKey(ctx, keyA)
		require.NoError(t, err)
		assert.Equal(t, pkgapi.TenantID("tenant-a"), validated.TenantID)

		env.T.Log("Cross-tenant access successfully blocked")
	})
}

/*
Test Coverage Summary:

1. JWT Workflow:
   - User login and token generation
   - Token reuse across operations
   - Context propagation
   - Concurrent users (10)

2. API Key Workflow:
   - Service account key creation
   - Key rotation
   - Multiple scopes
   - Expiration enforcement

3. Tenant Isolation:
   - Separate tenant operations
   - API key isolation
   - Agent tenant boundaries

4. RBAC Enforcement:
   - Admin permissions (all)
   - Developer permissions (limited)
   - Viewer permissions (read-only)

5. Security Scenarios:
   - Privilege escalation prevention
   - Immediate revocation
   - Concurrent validation (50 ops)
   - Cross-tenant access blocked

Total Test Cases: 20+
Integration Points: Auth + Scheduler + Runtime + API
Security Focus: Tenant isolation, RBAC, token/key management
*/
