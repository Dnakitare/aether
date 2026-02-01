// Package auth provides authentication and authorization.
package auth

import (
	"fmt"
)

// Role represents a user role.
type Role string

const (
	// RoleAdmin has full access to all resources.
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
)

// rolePermissions maps roles to their permissions.
var rolePermissions = map[Role][]Permission{
	RoleAdmin: {
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
	},
	RoleDeveloper: {
		PermissionAgentCreate,
		PermissionAgentRead,
		PermissionAgentUpdate,
		PermissionAgentDelete,
		PermissionQuotaRead,
		PermissionSchedulerRead,
		PermissionScalerRead,
	},
	RoleViewer: {
		PermissionAgentRead,
		PermissionQuotaRead,
		PermissionSchedulerRead,
		PermissionScalerRead,
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

// IsAdmin checks if claims represent an admin user.
func IsAdmin(claims *Claims) bool {
	return claims != nil && Role(claims.Role) == RoleAdmin
}
