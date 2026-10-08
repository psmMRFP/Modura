package authorization

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	casbin "github.com/casbin/casbin/v3"
	"github.com/casbin/casbin/v3/model"
	"github.com/jackc/pgx/v5"
	"github.com/modura-dev/modura/backend/internal/modules/identity"
)

// RolePolicies is the persisted policy snapshot for one assigned role.
type RolePolicies struct {
	RoleID   RoleID
	Policies []Policy
}

// ProvisioningStore is the persistence boundary consumed by authorization.
type ProvisioningStore interface {
	CreateReservedRole(context.Context, pgx.Tx, Role) error
	AssignRole(context.Context, pgx.Tx, identity.TenantID, identity.UserID, RoleID, time.Time) error
	LoadActorPolicies(context.Context, identity.Actor) ([]RolePolicies, error)
}

// Service exposes authorization-owned application operations.
type Service struct {
	store      ProvisioningStore
	management *managementDependencies
}

// NewService constructs an authorization service.
func NewService(store ProvisioningStore) (*Service, error) {
	if store == nil {
		return nil, fmt.Errorf("invalid authorization service configuration")
	}
	return &Service{store: store}, nil
}

// Authorize verifies a stable permission for a current tenant actor.
func (s *Service) Authorize(ctx context.Context, actor identity.Actor, permission Permission) error {
	roles, err := s.actorPolicies(ctx, actor, permission)
	if err != nil {
		return err
	}
	allowed, err := enforce(actor, permission, roles)
	if err != nil {
		return fmt.Errorf("evaluate authorization policy: %w", err)
	}
	if !allowed {
		return ErrDenied
	}
	return nil
}

// ResolveDataScope returns the union of every role scope granting permission.
func (s *Service) ResolveDataScope(ctx context.Context, actor identity.Actor, permission Permission) (ResolvedDataScope, error) {
	roles, err := s.actorPolicies(ctx, actor, permission)
	if err != nil {
		return ResolvedDataScope{}, err
	}
	allowed, err := enforce(actor, permission, roles)
	if err != nil {
		return ResolvedDataScope{}, fmt.Errorf("evaluate authorization policy: %w", err)
	}
	if !allowed {
		return ResolvedDataScope{}, ErrDenied
	}
	var resolved ResolvedDataScope
	custom := make(map[string]struct{})
	for _, role := range roles {
		for _, policy := range role.Policies {
			if policy.Permission != permission {
				continue
			}
			switch policy.Scope {
			case DataScopeAll:
				resolved.All = true
			case DataScopeSelf:
				resolved.Self = true
			case DataScopeDepartment:
				resolved.Department = true
			case DataScopeDepartmentDescendants:
				resolved.DepartmentAndDescendants = true
			case DataScopeCustom:
				for _, id := range policy.DepartmentIDs {
					custom[id] = struct{}{}
				}
			}
		}
	}
	for id := range custom {
		resolved.CustomDepartmentIDs = append(resolved.CustomDepartmentIDs, id)
	}
	slices.Sort(resolved.CustomDepartmentIDs)
	return resolved, nil
}

// EffectivePermissions returns the actor's canonical permission projection.
func (s *Service) EffectivePermissions(ctx context.Context, actor identity.Actor) ([]Permission, error) {
	if actor.TenantID == "" || actor.UserID == "" || actor.SessionID == "" {
		return nil, ErrDenied
	}
	roles, err := s.store.LoadActorPolicies(ctx, actor)
	if err != nil {
		return nil, fmt.Errorf("load effective permissions: %w", err)
	}
	seen := make(map[Permission]struct{})
	for _, role := range roles {
		for _, policy := range role.Policies {
			if knownPermission(policy.Permission) {
				seen[policy.Permission] = struct{}{}
			}
		}
	}
	permissions := make([]Permission, 0, len(seen))
	for permission := range seen {
		permissions = append(permissions, permission)
	}
	slices.SortFunc(permissions, func(a, b Permission) int {
		if comparison := strings.Compare(string(a.Resource), string(b.Resource)); comparison != 0 {
			return comparison
		}
		return strings.Compare(string(a.Action), string(b.Action))
	})
	return permissions, nil
}

func (s *Service) actorPolicies(ctx context.Context, actor identity.Actor, permission Permission) ([]RolePolicies, error) {
	if actor.TenantID == "" || actor.UserID == "" || actor.SessionID == "" || !knownPermission(permission) {
		return nil, ErrDenied
	}
	roles, err := s.store.LoadActorPolicies(ctx, actor)
	if err != nil {
		return nil, fmt.Errorf("load authorization policies: %w", err)
	}
	return roles, nil
}

func knownPermission(permission Permission) bool {
	actions := map[Action]bool{ActionRead: true, ActionCreate: true, ActionUpdate: true, ActionDelete: true}
	resources := map[Resource]bool{ResourceUsers: true, ResourceDepartments: true, ResourcePositions: true, ResourceUserOrganization: true, ResourceRoles: true, ResourcePolicies: true, ResourceUserRoles: true, ResourceDictionaries: true, ResourceConfigurations: true, ResourceAuditEvents: true}
	return actions[permission.Action] && resources[permission.Resource]
}

// IsKnownPermission reports whether a pair belongs to the canonical registry.
func IsKnownPermission(permission Permission) bool { return knownPermission(permission) }

func enforce(actor identity.Actor, permission Permission, roles []RolePolicies) (bool, error) {
	m := model.NewModel()
	m.AddDef("r", "r", "sub, dom, obj, act")
	m.AddDef("p", "p", "sub, dom, obj, act")
	m.AddDef("g", "g", "_, _, _")
	m.AddDef("e", "e", "some(where (p.eft == allow))")
	m.AddDef("m", "m", "g(r.sub, p.sub, r.dom) && r.dom == p.dom && r.obj == p.obj && r.act == p.act")
	e, err := casbin.NewEnforcer(m)
	if err != nil {
		return false, err
	}
	for _, role := range roles {
		if _, err := e.AddGroupingPolicy(string(actor.UserID), string(role.RoleID), string(actor.TenantID)); err != nil {
			return false, err
		}
		for _, policy := range role.Policies {
			if _, err := e.AddPolicy(string(role.RoleID), string(actor.TenantID), string(policy.Resource), string(policy.Action)); err != nil {
				return false, err
			}
		}
	}
	return e.Enforce(string(actor.UserID), string(actor.TenantID), string(permission.Resource), string(permission.Action))
}

// ProvisionTenantAdministrator creates and assigns the reserved tenant-admin role.
func (s *Service) ProvisionTenantAdministrator(ctx context.Context, tx pgx.Tx, role Role, userID identity.UserID) error {
	if role.TenantID == "" || role.ID == "" || role.Code != "tenant-admin" || userID == "" || !role.Reserved {
		return fmt.Errorf("invalid tenant administrator role")
	}
	if err := s.store.CreateReservedRole(ctx, tx, role); err != nil {
		return err
	}
	return s.store.AssignRole(ctx, tx, role.TenantID, userID, role.ID, role.CreatedAt)
}
