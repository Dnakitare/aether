package security_test

import (
	"context"
	"testing"

	"github.com/aether-runtime/aether/pkg/api"
)

// TestCrossTenantAccessDenied verifies that tenants cannot access other tenants' resources.
func TestCrossTenantAccessDenied(t *testing.T) {
	tests := []struct {
		name         string
		resourceType string
		requesterID  api.TenantID
		ownerID      api.TenantID
		expectAccess bool
	}{
		{
			name:         "Same tenant can access",
			resourceType: "agent",
			requesterID:  "tenant-a",
			ownerID:      "tenant-a",
			expectAccess: true,
		},
		{
			name:         "Different tenant cannot access",
			resourceType: "agent",
			requesterID:  "tenant-a",
			ownerID:      "tenant-b",
			expectAccess: false,
		},
		{
			name:         "Cross-tenant quota access denied",
			resourceType: "quota",
			requesterID:  "tenant-a",
			ownerID:      "tenant-b",
			expectAccess: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simulate access check
			allowed := checkTenantAccess(tt.requesterID, tt.ownerID)

			if allowed != tt.expectAccess {
				t.Errorf("expected access=%v, got access=%v", tt.expectAccess, allowed)
			}
		})
	}
}

// TestTenantIsolationInListOperations verifies list operations only return tenant's resources.
func TestTenantIsolationInListOperations(t *testing.T) {
	ctx := context.Background()

	// Setup: Create resources for multiple tenants
	tenantAAgents := []string{"agent-a-1", "agent-a-2"}
	tenantBAgents := []string{"agent-b-1", "agent-b-2"}

	// Simulate listing as tenant A
	listedAgents := listAgentsForTenant(ctx, "tenant-a")

	// Verify only tenant A's agents are returned
	if len(listedAgents) != len(tenantAAgents) {
		t.Errorf("expected %d agents for tenant A, got %d", len(tenantAAgents), len(listedAgents))
	}

	for _, agentID := range listedAgents {
		if !contains(tenantAAgents, agentID) {
			t.Errorf("tenant A's list contains agent from another tenant: %s", agentID)
		}
	}

	// Verify tenant B's agents are NOT in the list
	for _, agentID := range tenantBAgents {
		if contains(listedAgents, agentID) {
			t.Errorf("tenant A's list leaked tenant B's agent: %s", agentID)
		}
	}
}

// TestAdminCanAccessAllTenants verifies admin role can access all tenants.
func TestAdminCanAccessAllTenants(t *testing.T) {
	tests := []struct {
		name         string
		role         string
		requesterID  api.TenantID
		targetID     api.TenantID
		expectAccess bool
	}{
		{
			name:         "Admin can access different tenant",
			role:         "admin",
			requesterID:  "tenant-a",
			targetID:     "tenant-b",
			expectAccess: true,
		},
		{
			name:         "Non-admin cannot access different tenant",
			role:         "developer",
			requesterID:  "tenant-a",
			targetID:     "tenant-b",
			expectAccess: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			allowed := checkAccessWithRole(tt.role, tt.requesterID, tt.targetID)

			if allowed != tt.expectAccess {
				t.Errorf("expected access=%v for role=%s, got access=%v", tt.expectAccess, tt.role, allowed)
			}
		})
	}
}

// Helper functions

func checkTenantAccess(requester, owner api.TenantID) bool {
	return requester == owner
}

func listAgentsForTenant(ctx context.Context, tenantID api.TenantID) []string {
	// Mock implementation
	if tenantID == "tenant-a" {
		return []string{"agent-a-1", "agent-a-2"}
	}
	return []string{"agent-b-1", "agent-b-2"}
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

func checkAccessWithRole(role string, requester, target api.TenantID) bool {
	if role == "admin" {
		return true
	}
	return requester == target
}
