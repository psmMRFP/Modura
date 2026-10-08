package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/modura-dev/modura/backend/internal/modules/authorization"
	authorizationdb "github.com/modura-dev/modura/backend/internal/modules/authorization/postgres/db"
	"github.com/modura-dev/modura/backend/internal/modules/identity"
)

// ListRoles returns tenant roles without leaking roles from another tenant.
func (s Store) ListRoles(ctx context.Context, tenantID identity.TenantID) ([]authorization.RoleView, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, code, name, reserved, version FROM modura.roles WHERE tenant_id = $1 ORDER BY reserved DESC, code, id`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("query roles: %w", err)
	}
	defer rows.Close()
	roles := make([]authorization.RoleView, 0)
	for rows.Next() {
		var role authorization.RoleView
		if err := rows.Scan(&role.ID, &role.Code, &role.Name, &role.Reserved, &role.Version); err != nil {
			return nil, fmt.Errorf("scan role: %w", err)
		}
		roles = append(roles, role)
	}
	return roles, rows.Err()
}

// CreateRole inserts a non-reserved tenant role in a caller transaction.
func (Store) CreateRole(ctx context.Context, tx pgx.Tx, role authorization.Role) error {
	_, err := tx.Exec(ctx, `INSERT INTO modura.roles (id, tenant_id, code, name, reserved, version, created_at, updated_at) VALUES ($1, $2, $3, $4, false, 1, $5, $5)`, role.ID, role.TenantID, role.Code, role.Name, role.CreatedAt)
	if err != nil {
		return fmt.Errorf("insert role: %w", err)
	}
	return nil
}

// ReplaceRolePolicies replaces one non-reserved role with optimistic locking.
func (Store) ReplaceRolePolicies(ctx context.Context, tx pgx.Tx, tenantID identity.TenantID, roleID authorization.RoleID, expectedVersion int64, desired []authorization.Policy, now time.Time) ([]authorization.Policy, int64, error) {
	var reserved bool
	var version int64
	if err := tx.QueryRow(ctx, `SELECT reserved, version FROM modura.roles WHERE tenant_id = $1 AND id = $2 FOR UPDATE`, tenantID, roleID).Scan(&reserved, &version); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, 0, authorization.ErrNotFound
		}
		return nil, 0, fmt.Errorf("lock role: %w", err)
	}
	if reserved {
		return nil, 0, authorization.ErrReserved
	}
	if version != expectedVersion {
		return nil, 0, authorization.ErrConflict
	}
	before, err := loadRolePolicies(ctx, tx, tenantID, roleID)
	if err != nil {
		return nil, 0, err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM modura.role_policies WHERE tenant_id = $1 AND role_id = $2`, tenantID, roleID); err != nil {
		return nil, 0, fmt.Errorf("delete role policies: %w", err)
	}
	for _, policy := range desired {
		if _, err := tx.Exec(ctx, `INSERT INTO modura.role_policies (tenant_id, role_id, resource, action, data_scope, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6, $6)`, tenantID, roleID, policy.Resource, policy.Action, policy.Scope, now); err != nil {
			return nil, 0, fmt.Errorf("insert role policy: %w", err)
		}
		for _, departmentID := range policy.DepartmentIDs {
			if _, err := tx.Exec(ctx, `INSERT INTO modura.role_policy_departments (tenant_id, role_id, resource, action, department_id) VALUES ($1, $2, $3, $4, $5)`, tenantID, roleID, policy.Resource, policy.Action, departmentID); err != nil {
				return nil, 0, fmt.Errorf("insert custom policy department: %w", err)
			}
		}
	}
	version++
	if _, err := tx.Exec(ctx, `UPDATE modura.roles SET version = $3, updated_at = $4 WHERE tenant_id = $1 AND id = $2`, tenantID, roleID, version, now); err != nil {
		return nil, 0, fmt.Errorf("update role version: %w", err)
	}
	return before, version, nil
}

func loadRolePolicies(ctx context.Context, tx pgx.Tx, tenantID identity.TenantID, roleID authorization.RoleID) ([]authorization.Policy, error) {
	rows, err := tx.Query(ctx, `SELECT rp.resource, rp.action, rp.data_scope, COALESCE(array_agg(rpd.department_id::text ORDER BY rpd.department_id) FILTER (WHERE rpd.department_id IS NOT NULL), ARRAY[]::text[]) FROM modura.role_policies rp LEFT JOIN modura.role_policy_departments rpd ON rpd.tenant_id = rp.tenant_id AND rpd.role_id = rp.role_id AND rpd.resource = rp.resource AND rpd.action = rp.action WHERE rp.tenant_id = $1 AND rp.role_id = $2 GROUP BY rp.resource, rp.action, rp.data_scope ORDER BY rp.resource, rp.action`, tenantID, roleID)
	if err != nil {
		return nil, fmt.Errorf("query role policies: %w", err)
	}
	defer rows.Close()
	var policies []authorization.Policy
	for rows.Next() {
		var policy authorization.Policy
		if err := rows.Scan(&policy.Resource, &policy.Action, &policy.Scope, &policy.DepartmentIDs); err != nil {
			return nil, fmt.Errorf("scan role policy: %w", err)
		}
		policies = append(policies, policy)
	}
	return policies, rows.Err()
}

// GetRolePolicySet returns one tenant role's versioned policy state.
func (s Store) GetRolePolicySet(ctx context.Context, tenantID identity.TenantID, roleID authorization.RoleID) (authorization.RolePolicySet, error) {
	var state authorization.RolePolicySet
	if err := s.pool.QueryRow(ctx, `SELECT reserved, version FROM modura.roles WHERE tenant_id = $1 AND id = $2`, tenantID, roleID).Scan(&state.Reserved, &state.Version); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return authorization.RolePolicySet{}, authorization.ErrNotFound
		}
		return authorization.RolePolicySet{}, fmt.Errorf("query role policy state: %w", err)
	}
	rows, err := s.pool.Query(ctx, `SELECT rp.resource, rp.action, rp.data_scope, COALESCE(array_agg(rpd.department_id::text ORDER BY rpd.department_id) FILTER (WHERE rpd.department_id IS NOT NULL), ARRAY[]::text[]) FROM modura.role_policies rp LEFT JOIN modura.role_policy_departments rpd ON rpd.tenant_id = rp.tenant_id AND rpd.role_id = rp.role_id AND rpd.resource = rp.resource AND rpd.action = rp.action WHERE rp.tenant_id = $1 AND rp.role_id = $2 GROUP BY rp.resource, rp.action, rp.data_scope ORDER BY rp.resource, rp.action`, tenantID, roleID)
	if err != nil {
		return authorization.RolePolicySet{}, fmt.Errorf("query role policy state: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var policy authorization.Policy
		if err := rows.Scan(&policy.Resource, &policy.Action, &policy.Scope, &policy.DepartmentIDs); err != nil {
			return authorization.RolePolicySet{}, fmt.Errorf("scan role policy state: %w", err)
		}
		state.Policies = append(state.Policies, policy)
	}
	return state, rows.Err()
}

// GetUserRoleGrants returns version 1 for a valid user with no prior grants.
// User existence is verified by the identity module before this read.
func (s Store) GetUserRoleGrants(ctx context.Context, tx pgx.Tx, tenantID identity.TenantID, userID identity.UserID) (authorization.UserRoleGrantSet, error) {
	queries := authorizationdb.New(tx)
	state := authorization.UserRoleGrantSet{Version: 1, RoleIDs: []authorization.RoleID{}}
	version, err := queries.UserRoleVersion(ctx, authorizationdb.UserRoleVersionParams{TenantID: string(tenantID), UserID: string(userID)})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return authorization.UserRoleGrantSet{}, fmt.Errorf("query user role version: %w", err)
	}
	if err == nil {
		state.Version = version
	}
	roleIDs, err := queries.NonReservedRoleIDsByUser(ctx, authorizationdb.NonReservedRoleIDsByUserParams{TenantID: string(tenantID), UserID: string(userID)})
	if err != nil {
		return authorization.UserRoleGrantSet{}, fmt.Errorf("query user roles: %w", err)
	}
	for _, roleID := range roleIDs {
		state.RoleIDs = append(state.RoleIDs, authorization.RoleID(roleID))
	}
	return state, nil
}

// ReplaceUserRoleGrants replaces non-reserved role grants with optimistic
// locking. User existence is verified by the identity module beforehand.
func (Store) ReplaceUserRoleGrants(ctx context.Context, tx pgx.Tx, tenantID identity.TenantID, userID identity.UserID, expectedVersion int64, desired []authorization.RoleID, now time.Time) (authorization.UserRoleGrantSet, error) {
	queries := authorizationdb.New(tx)
	if _, err := queries.InitializeUserRoleVersion(ctx, authorizationdb.InitializeUserRoleVersionParams{TenantID: string(tenantID), UserID: string(userID), UpdatedAt: now}); err != nil {
		return authorization.UserRoleGrantSet{}, fmt.Errorf("initialize user role version: %w", err)
	}
	version, err := queries.LockUserRoleVersion(ctx, authorizationdb.LockUserRoleVersionParams{TenantID: string(tenantID), UserID: string(userID)})
	if err != nil {
		return authorization.UserRoleGrantSet{}, fmt.Errorf("lock user role version: %w", err)
	}
	if version != expectedVersion {
		return authorization.UserRoleGrantSet{}, authorization.ErrConflict
	}
	before := authorization.UserRoleGrantSet{Version: version, RoleIDs: []authorization.RoleID{}}
	rows, err := tx.Query(ctx, `SELECT ur.role_id FROM modura.user_roles ur JOIN modura.roles r ON r.tenant_id = ur.tenant_id AND r.id = ur.role_id WHERE ur.tenant_id = $1 AND ur.user_id = $2 AND r.reserved = false ORDER BY ur.role_id`, tenantID, userID)
	if err != nil {
		return authorization.UserRoleGrantSet{}, fmt.Errorf("query previous user roles: %w", err)
	}
	for rows.Next() {
		var roleID authorization.RoleID
		if err := rows.Scan(&roleID); err != nil {
			rows.Close()
			return authorization.UserRoleGrantSet{}, fmt.Errorf("scan previous user role: %w", err)
		}
		before.RoleIDs = append(before.RoleIDs, roleID)
	}
	rows.Close()
	if len(desired) > 0 {
		var validCount int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM modura.roles WHERE tenant_id = $1 AND id = ANY($2::uuid[]) AND reserved = false`, tenantID, roleStrings(desired)).Scan(&validCount); err != nil {
			return authorization.UserRoleGrantSet{}, fmt.Errorf("validate desired roles: %w", err)
		}
		if validCount != len(desired) {
			return authorization.UserRoleGrantSet{}, authorization.ErrDenied
		}
	}
	if _, err := tx.Exec(ctx, `DELETE FROM modura.user_roles WHERE tenant_id = $1 AND user_id = $2 AND role_id IN (SELECT id FROM modura.roles WHERE tenant_id = $1 AND reserved = false)`, tenantID, userID); err != nil {
		return authorization.UserRoleGrantSet{}, fmt.Errorf("delete user role grants: %w", err)
	}
	for _, roleID := range desired {
		if _, err := tx.Exec(ctx, `INSERT INTO modura.user_roles (tenant_id, user_id, role_id, created_at) VALUES ($1, $2, $3, $4)`, tenantID, userID, roleID, now); err != nil {
			return authorization.UserRoleGrantSet{}, fmt.Errorf("insert user role grant: %w", err)
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE modura.user_role_versions SET version = version + 1, updated_at = $3 WHERE tenant_id = $1 AND user_id = $2`, tenantID, userID, now); err != nil {
		return authorization.UserRoleGrantSet{}, fmt.Errorf("update user role version: %w", err)
	}
	return before, nil
}

func roleStrings(ids []authorization.RoleID) []string {
	values := make([]string, len(ids))
	for i, id := range ids {
		values[i] = string(id)
	}
	return values
}
