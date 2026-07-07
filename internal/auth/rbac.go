// Package auth provides authentication and authorization.
package auth

import (
	"fmt"
)

// Role represents a user role.
type Role string

const (
	// RolePlatformAdmin operates the platform itself: it can act across all
	// tenants (set any tenant's quota, view cluster topology, manage global
	// scaling policies). Granted only via the "platform:admin" scope, which
	// tenant-issued API keys must never carry.
	RolePlatformAdmin Role = "platform_admin"

	// RoleAdmin administers a single tenant: full agent lifecycle plus
	// read-only quota within that tenant. It is NOT a platform operator and
	// cannot reach other tenants or raise its own quota.
	RoleAdmin Role = "admin"

	// RoleDeveloper can create and manage agents within their tenant.
	RoleDeveloper Role = "developer"

	// RoleViewer can only read resources.
	RoleViewer Role = "viewer"
)

// Permission represents an action that can be performed.
type Permission string

const (
	// PermissionAgentCreate allows creating agents.
	PermissionAgentCreate Permission = "agent:create"

	// PermissionAgentRead allows reading agent info.
	PermissionAgentRead Permission = "agent:read"

	// PermissionAgentUpdate allows updating agents.
	PermissionAgentUpdate Permission = "agent:update"

	// PermissionAgentDelete allows deleting agents.
	PermissionAgentDelete Permission = "agent:delete"

	// PermissionQuotaRead allows reading quotas.
	PermissionQuotaRead Permission = "quota:read"

	// PermissionQuotaWrite allows modifying quotas.
	PermissionQuotaWrite Permission = "quota:write"

	// PermissionSchedulerRead allows reading scheduler state.
	PermissionSchedulerRead Permission = "scheduler:read"

	// PermissionSchedulerWrite allows modifying scheduler.
	PermissionSchedulerWrite Permission = "scheduler:write"

	// PermissionScalerRead allows reading scaling policies.
	PermissionScalerRead Permission = "scaler:read"

	// PermissionScalerWrite allows modifying scaling policies.
	PermissionScalerWrite Permission = "scaler:write"

	// PermissionPlatformAdmin allows cross-tenant, platform-level operations
	// (setting any tenant's quota, viewing cluster topology, managing global
	// scaling policies). Held only by RolePlatformAdmin.
	PermissionPlatformAdmin Permission = "platform:admin"
)

// rolePermissions maps roles to their permissions.
//
// Tenant roles (admin/developer/viewer) are deliberately scoped to a single
// tenant: quota writes, cross-tenant reads, cluster topology, and global
// scaler policies all require PermissionPlatformAdmin, which only
// RolePlatformAdmin carries. This prevents a tenant key from reading or
// mutating another tenant's state or raising its own quota.
var rolePermissions = map[Role][]Permission{
	RolePlatformAdmin: {
		PermissionAgentCreate,
		PermissionAgentRead,
		PermissionAgentUpdate,
		PermissionAgentDelete,
		PermissionQuotaRead,
		PermissionQuotaWrite,
		PermissionSchedulerRead,
		PermissionSchedulerWrite,
		PermissionScalerRead,
		PermissionScalerWrite,
		PermissionPlatformAdmin,
	},
	RoleAdmin: {
		PermissionAgentCreate,
		PermissionAgentRead,
		PermissionAgentUpdate,
		PermissionAgentDelete,
		PermissionQuotaRead,
	},
	RoleDeveloper: {
		PermissionAgentCreate,
		PermissionAgentRead,
		PermissionAgentUpdate,
		PermissionAgentDelete,
		PermissionQuotaRead,
	},
	RoleViewer: {
		PermissionAgentRead,
		PermissionQuotaRead,
	},
}

// HasPermission checks if a role has a specific permission.
func HasPermission(role Role, permission Permission) bool {
	permissions, exists := rolePermissions[role]
	if !exists {
		return false
	}

	for _, p := range permissions {
		if p == permission {
			return true
		}
	}

	return false
}

// CheckPermission checks if claims have a specific permission.
func CheckPermission(claims *Claims, permission Permission) error {
	if claims == nil {
		return fmt.Errorf("no claims provided")
	}

	role := Role(claims.Role)
	if !HasPermission(role, permission) {
		return fmt.Errorf("role %s does not have permission %s", role, permission)
	}

	return nil
}

// IsAdmin checks if claims represent a tenant admin (or platform admin, which
// is a superset). Use this only for operations scoped to the caller's own
// tenant; cross-tenant operations must use IsPlatformAdmin.
func IsAdmin(claims *Claims) bool {
	if claims == nil {
		return false
	}
	role := Role(claims.Role)
	return role == RoleAdmin || role == RolePlatformAdmin
}

// IsPlatformAdmin reports whether claims carry platform-operator authority,
// required for any cross-tenant or cluster-wide operation.
func IsPlatformAdmin(claims *Claims) bool {
	return claims != nil && Role(claims.Role) == RolePlatformAdmin
}
