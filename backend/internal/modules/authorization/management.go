package authorization

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/psmMRFP/WhereToLive/backend/internal/modules/audit"
	"github.com/psmMRFP/WhereToLive/backend/internal/modules/identity"
)

// ManagementStore persists tenant role and policy desired state.
type ManagementStore interface {
	ListRoles(context.Context, identity.TenantID) ([]RoleView, error)
	CreateRole(context.Context, pgx.Tx, Role) error
	ReplaceRolePolicies(context.Context, pgx.Tx, identity.TenantID, RoleID, int64, []Policy, time.Time) ([]Policy, int64, error)
	GetUserRoleGrants(context.Context, pgx.Tx, identity.TenantID, identity.UserID) (UserRoleGrantSet, error)
	GetRolePolicySet(context.Context, identity.TenantID, RoleID) (RolePolicySet, error)
	ReplaceUserRoleGrants(context.Context, pgx.Tx, identity.TenantID, identity.UserID, int64, []RoleID, time.Time) (UserRoleGrantSet, error)
}

// Transactor supplies application-owned transaction boundaries.
type Transactor interface {
	WithinTransaction(context.Context, func(pgx.Tx) error) error
}

// Auditor records authorization writes in the same transaction.
type Auditor interface {
	RecordTenantWrite(context.Context, pgx.Tx, audit.Event) error
}

// UserExistence verifies tenant-local account existence through the owning
// identity module instead of reading its tables directly.
type UserExistence interface {
	UserExists(context.Context, pgx.Tx, identity.TenantID, identity.UserID) (bool, error)
}

// EnableManagement adds authorization-management use cases to a service.
func (s *Service) EnableManagement(store ManagementStore, transactions Transactor, auditor Auditor, identities UserExistence, now func() time.Time, newID func(time.Time) (string, error)) error {
	if store == nil || transactions == nil || auditor == nil || identities == nil || now == nil || newID == nil {
		return fmt.Errorf("invalid authorization management configuration")
	}
	s.management = &managementDependencies{store: store, transactions: transactions, auditor: auditor, identities: identities, now: now, newID: newID}
	return nil
}

type managementDependencies struct {
	store        ManagementStore
	transactions Transactor
	auditor      Auditor
	identities   UserExistence
	now          func() time.Time
	newID        func(time.Time) (string, error)
}

// ListRoles lists roles only in the verified actor tenant.
func (s *Service) ListRoles(ctx context.Context, actor identity.Actor) ([]RoleView, error) {
	if !validActor(actor) || s.management == nil {
		return nil, ErrDenied
	}
	return s.management.store.ListRoles(ctx, actor.TenantID)
}

// GetRolePolicySet returns a role's versioned policy state.
func (s *Service) GetRolePolicySet(ctx context.Context, actor identity.Actor, roleID RoleID) (RolePolicySet, error) {
	if !validActor(actor) || s.management == nil || roleID == "" {
		return RolePolicySet{}, ErrDenied
	}
	return s.management.store.GetRolePolicySet(ctx, actor.TenantID, roleID)
}

// CreateRole creates an empty, non-reserved tenant role.
func (s *Service) CreateRole(ctx context.Context, write WriteContext, code, name string) (RoleView, error) {
	if !validWrite(write) || s.management == nil {
		return RoleView{}, ErrDenied
	}
	code = strings.ToLower(strings.TrimSpace(code))
	name = strings.TrimSpace(name)
	if code == "" || name == "" || code == "tenant-admin" {
		return RoleView{}, fmt.Errorf("invalid role")
	}
	now := s.management.now().UTC()
	id, err := s.management.newID(now)
	if err != nil {
		return RoleView{}, fmt.Errorf("generate role ID: %w", err)
	}
	role := Role{ID: RoleID(id), TenantID: write.Actor.TenantID, Code: code, Name: name, CreatedAt: now, Version: 1}
	err = s.management.transactions.WithinTransaction(ctx, func(tx pgx.Tx) error {
		if err := s.management.store.CreateRole(ctx, tx, role); err != nil {
			return err
		}
		return s.auditChange(ctx, tx, write, now, "authorization.role.created", "role", id, nil, RoleView{ID: role.ID, Code: role.Code, Name: role.Name, Version: 1})
	})
	if err != nil {
		return RoleView{}, fmt.Errorf("create role: %w", err)
	}
	return RoleView{ID: role.ID, Code: role.Code, Name: role.Name, Version: 1}, nil
}

// ReplaceRolePolicies atomically replaces a non-reserved role's policies.
func (s *Service) ReplaceRolePolicies(ctx context.Context, write WriteContext, roleID RoleID, expectedVersion int64, desired []Policy) (int64, error) {
	if !validWrite(write) || s.management == nil || roleID == "" || expectedVersion < 1 || !validPolicies(desired) {
		return 0, fmt.Errorf("invalid role policies")
	}
	for _, policy := range desired {
		resolved, err := s.ResolveDataScope(ctx, write.Actor, policy.Permission)
		if err != nil || !canDelegate(resolved, policy) {
			return 0, ErrDenied
		}
	}
	now := s.management.now().UTC()
	var before []Policy
	var version int64
	err := s.management.transactions.WithinTransaction(ctx, func(tx pgx.Tx) error {
		var err error
		before, version, err = s.management.store.ReplaceRolePolicies(ctx, tx, write.Actor.TenantID, roleID, expectedVersion, desired, now)
		if err != nil {
			return err
		}
		return s.auditChange(ctx, tx, write, now, "authorization.role-policies.replaced", "role", string(roleID), before, desired)
	})
	if err != nil {
		return 0, fmt.Errorf("replace role policies: %w", err)
	}
	return version, nil
}

// GetUserRoleGrants returns a user's versioned tenant-local role state.
func (s *Service) GetUserRoleGrants(ctx context.Context, actor identity.Actor, userID identity.UserID) (UserRoleGrantSet, error) {
	if !validActor(actor) || s.management == nil || userID == "" {
		return UserRoleGrantSet{}, ErrDenied
	}
	var state UserRoleGrantSet
	err := s.management.transactions.WithinTransaction(ctx, func(tx pgx.Tx) error {
		exists, err := s.management.identities.UserExists(ctx, tx, actor.TenantID, userID)
		if err != nil {
			return err
		}
		if !exists {
			return ErrNotFound
		}
		state, err = s.management.store.GetUserRoleGrants(ctx, tx, actor.TenantID, userID)
		return err
	})
	if err != nil {
		return UserRoleGrantSet{}, err
	}
	return state, nil
}

// ReplaceUserRoleGrants applies desired state with optimistic locking and audit snapshots.
func (s *Service) ReplaceUserRoleGrants(ctx context.Context, write WriteContext, userID identity.UserID, expectedVersion int64, desired []RoleID) (UserRoleGrantSet, error) {
	if !validWrite(write) || s.management == nil || userID == "" || expectedVersion < 1 {
		return UserRoleGrantSet{}, fmt.Errorf("invalid user role grants")
	}
	desired = append([]RoleID(nil), desired...)
	slices.Sort(desired)
	if slices.ContainsFunc(desired, func(id RoleID) bool { return id == "" }) || hasDuplicateRoleIDs(desired) {
		return UserRoleGrantSet{}, fmt.Errorf("invalid user role grants")
	}
	for _, roleID := range desired {
		policySet, err := s.management.store.GetRolePolicySet(ctx, write.Actor.TenantID, roleID)
		if err != nil {
			return UserRoleGrantSet{}, err
		}
		if policySet.Reserved {
			return UserRoleGrantSet{}, ErrReserved
		}
		for _, policy := range policySet.Policies {
			resolved, err := s.ResolveDataScope(ctx, write.Actor, policy.Permission)
			if err != nil || !canDelegate(resolved, policy) {
				return UserRoleGrantSet{}, ErrDenied
			}
		}
	}
	now := s.management.now().UTC()
	var before UserRoleGrantSet
	var after UserRoleGrantSet
	err := s.management.transactions.WithinTransaction(ctx, func(tx pgx.Tx) error {
		exists, err := s.management.identities.UserExists(ctx, tx, write.Actor.TenantID, userID)
		if err != nil {
			return err
		}
		if !exists {
			return ErrNotFound
		}
		before, err = s.management.store.ReplaceUserRoleGrants(ctx, tx, write.Actor.TenantID, userID, expectedVersion, desired, now)
		if err != nil {
			return err
		}
		after = UserRoleGrantSet{Version: before.Version + 1, RoleIDs: desired}
		return s.auditChange(ctx, tx, write, now, "authorization.user-roles.replaced", "user", string(userID), before, after)
	})
	if err != nil {
		return UserRoleGrantSet{}, fmt.Errorf("replace user role grants: %w", err)
	}
	return after, nil
}

func (s *Service) auditChange(ctx context.Context, tx pgx.Tx, write WriteContext, now time.Time, action, resource, resourceID string, before, after any) error {
	beforeJSON, err := json.Marshal(before)
	if err != nil {
		return fmt.Errorf("encode before audit state: %w", err)
	}
	afterJSON, err := json.Marshal(after)
	if err != nil {
		return fmt.Errorf("encode after audit state: %w", err)
	}
	return s.management.auditor.RecordTenantWrite(ctx, tx, audit.Event{ActorID: write.Actor.UserID, TenantID: write.Actor.TenantID, Action: action, Resource: resource, ResourceID: resourceID, Reason: "authorized tenant management request", CorrelationID: write.CorrelationID, OccurredAt: now, BeforeState: beforeJSON, AfterState: afterJSON})
}

func validActor(actor identity.Actor) bool {
	return actor.TenantID != "" && actor.UserID != "" && actor.SessionID != ""
}

func validWrite(write WriteContext) bool {
	return validActor(write.Actor) && strings.TrimSpace(write.CorrelationID) != ""
}

func validPolicies(policies []Policy) bool {
	seen := make(map[Permission]struct{}, len(policies))
	for _, policy := range policies {
		if !knownPermission(policy.Permission) {
			return false
		}
		if _, exists := seen[policy.Permission]; exists {
			return false
		}
		seen[policy.Permission] = struct{}{}
		if policy.Resource != ResourceDepartments && policy.Resource != ResourceUserOrganization && policy.Scope != DataScopeAll {
			return false
		}
		if policy.Scope == DataScopeCustom {
			if len(policy.DepartmentIDs) == 0 || duplicateStrings(policy.DepartmentIDs) {
				return false
			}
		} else if (policy.Scope != DataScopeAll && policy.Scope != DataScopeSelf && policy.Scope != DataScopeDepartment && policy.Scope != DataScopeDepartmentDescendants) || len(policy.DepartmentIDs) != 0 {
			return false
		}
	}
	return true
}

func duplicateStrings(values []string) bool {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			return true
		}
		if _, exists := seen[value]; exists {
			return true
		}
		seen[value] = struct{}{}
	}
	return false
}

func hasDuplicateRoleIDs(ids []RoleID) bool {
	for i := 1; i < len(ids); i++ {
		if ids[i] == ids[i-1] {
			return true
		}
	}
	return false
}

func canDelegate(granted ResolvedDataScope, desired Policy) bool {
	if granted.All {
		return true
	}
	switch desired.Scope {
	case DataScopeAll:
		return false
	case DataScopeSelf:
		return granted.Self || granted.Department || granted.DepartmentAndDescendants
	case DataScopeDepartment:
		return granted.Department || granted.DepartmentAndDescendants
	case DataScopeDepartmentDescendants:
		return granted.DepartmentAndDescendants
	case DataScopeCustom:
		for _, id := range desired.DepartmentIDs {
			if !slices.Contains(granted.CustomDepartmentIDs, id) {
				return false
			}
		}
		return true
	default:
		return false
	}
}
