// Package authorization owns tenant roles, grants, and policy evaluation.
package authorization

import (
	"errors"
	"time"

	"github.com/modura-dev/modura/backend/internal/modules/identity"
)

var (
	// ErrDenied means the actor lacks the requested stable permission.
	ErrDenied = errors.New("authorization denied")
	// ErrNotFound hides cross-tenant authorization resources.
	ErrNotFound = errors.New("authorization resource not found")
	// ErrConflict means an expected version is stale.
	ErrConflict = errors.New("authorization version conflict")
	// ErrReserved means a tenant tried to mutate a platform-owned role.
	ErrReserved = errors.New("reserved authorization resource")
)

// Resource is a stable authorization resource identifier.
type Resource string

// Action is a stable authorization action identifier.
type Action string

const (
	// ResourceUsers protects tenant user administration.
	ResourceUsers Resource = "identity.users"
	// ResourceDepartments protects department management.
	ResourceDepartments Resource = "organization.departments"
	// ResourcePositions protects position management.
	ResourcePositions Resource = "organization.positions"
	// ResourceUserOrganization protects user organization assignments.
	ResourceUserOrganization Resource = "organization.user-organization"
	// ResourceRoles protects tenant role management.
	ResourceRoles Resource = "authorization.roles"
	// ResourcePolicies protects tenant role policy management.
	ResourcePolicies Resource = "authorization.policies"
	// ResourceUserRoles protects desired-state user role grants.
	ResourceUserRoles Resource = "authorization.user-roles"
	// ResourceDictionaries protects tenant dictionary management.
	ResourceDictionaries Resource = "settings.dictionaries"
	// ResourceConfigurations protects non-secret tenant configuration.
	ResourceConfigurations Resource = "settings.configurations"
	// ResourceAuditEvents protects tenant audit review.
	ResourceAuditEvents Resource = "audit.events"
	// ActionRead permits viewing a resource.
	ActionRead Action = "read"
	// ActionCreate permits creating a resource.
	ActionCreate Action = "create"
	// ActionUpdate permits changing a resource.
	ActionUpdate Action = "update"
	// ActionDelete permits deleting a resource.
	ActionDelete Action = "delete"
)

// Permission is the stable resource/action pair checked by the server.
type Permission struct {
	Resource Resource
	Action   Action
}

// DataScopeKind is a closed, SQL-free repository visibility mode.
type DataScopeKind string

const (
	// DataScopeAll permits every tenant row for the protected resource.
	DataScopeAll DataScopeKind = "all"
	// DataScopeSelf permits only the actor's own row or primary department.
	DataScopeSelf DataScopeKind = "self"
	// DataScopeDepartment permits the actor's primary department.
	DataScopeDepartment DataScopeKind = "department"
	// DataScopeDepartmentDescendants includes the primary department subtree.
	DataScopeDepartmentDescendants DataScopeKind = "department-and-descendants"
	// DataScopeCustom permits an explicit set of same-tenant departments.
	DataScopeCustom DataScopeKind = "custom"
)

// Policy is a tenant role's functional permission and data scope.
type Policy struct {
	Permission
	Scope         DataScopeKind
	DepartmentIDs []string
}

// ResolvedDataScope is the typed union passed to an owning repository.
type ResolvedDataScope struct {
	All                      bool
	Self                     bool
	Department               bool
	DepartmentAndDescendants bool
	CustomDepartmentIDs      []string
}

// RoleID identifies a tenant-owned role.
type RoleID string

// Role is the state required to provision a role.
type Role struct {
	ID        RoleID
	TenantID  identity.TenantID
	Code      string
	Name      string
	Reserved  bool
	CreatedAt time.Time
	Version   int64
}

// RoleView is the stable role projection returned to transports.
type RoleView struct {
	ID       RoleID
	Code     string
	Name     string
	Reserved bool
	Version  int64
}

// WriteContext carries verified evidence for authorization management writes.
type WriteContext struct {
	Actor         identity.Actor
	CorrelationID string
}

// UserRoleGrantSet is the versioned desired state of a user's role assignments.
type UserRoleGrantSet struct {
	Version int64
	RoleIDs []RoleID
}

// RolePolicySet is a role's versioned policy desired state.
type RolePolicySet struct {
	Version  int64
	Reserved bool
	Policies []Policy
}
