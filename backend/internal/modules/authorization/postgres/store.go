// Package postgres persists authorization-owned data in PostgreSQL.
package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/modura-dev/modura/backend/internal/modules/authorization"
	"github.com/modura-dev/modura/backend/internal/modules/identity"
)

// Store persists authorization data inside caller-owned workflow transactions.
type Store struct{ pool *pgxpool.Pool }

// New constructs an authorization store.
func New(pool *pgxpool.Pool) Store { return Store{pool: pool} }

// CreateReservedRole inserts the tenant administrator and its fixed policies.
func (Store) CreateReservedRole(ctx context.Context, tx pgx.Tx, role authorization.Role) error {
	_, err := tx.Exec(ctx, `INSERT INTO modura.roles (id, tenant_id, code, name, reserved, created_at, updated_at) VALUES ($1, $2, $3, $4, true, $5, $5)`, role.ID, role.TenantID, role.Code, role.Name, role.CreatedAt)
	if err != nil {
		return fmt.Errorf("insert reserved role: %w", err)
	}
	for _, permission := range tenantAdministratorPermissions() {
		_, err = tx.Exec(ctx, `INSERT INTO modura.role_policies (tenant_id, role_id, resource, action, data_scope, created_at, updated_at) VALUES ($1, $2, $3, $4, 'all', $5, $5)`, role.TenantID, role.ID, permission.Resource, permission.Action, role.CreatedAt)
		if err != nil {
			return fmt.Errorf("insert reserved role policy: %w", err)
		}
	}
	return nil
}

// AssignRole grants a tenant role to a tenant-local user in the workflow transaction.
func (Store) AssignRole(ctx context.Context, tx pgx.Tx, tenantID identity.TenantID, userID identity.UserID, roleID authorization.RoleID, now time.Time) error {
	_, err := tx.Exec(ctx, `INSERT INTO modura.user_roles (tenant_id, user_id, role_id, created_at) VALUES ($1, $2, $3, $4)`, tenantID, userID, roleID, now)
	if err != nil {
		return fmt.Errorf("assign role: %w", err)
	}
	_, err = tx.Exec(ctx, `INSERT INTO modura.user_role_versions (tenant_id, user_id, version, updated_at) VALUES ($1, $2, 1, $3) ON CONFLICT (tenant_id, user_id) DO NOTHING`, tenantID, userID, now)
	if err != nil {
		return fmt.Errorf("initialize user role version: %w", err)
	}
	return nil
}

// LoadActorPolicies loads only roles and policies inside the verified tenant.
func (s Store) LoadActorPolicies(ctx context.Context, actor identity.Actor) ([]authorization.RolePolicies, error) {
	if s.pool == nil {
		return nil, nil
	}
	rows, err := s.pool.Query(ctx, `SELECT ur.role_id, rp.resource, rp.action, rp.data_scope, COALESCE(array_agg(rpd.department_id::text ORDER BY rpd.department_id) FILTER (WHERE rpd.department_id IS NOT NULL), ARRAY[]::text[]) FROM modura.user_roles ur JOIN modura.role_policies rp ON rp.tenant_id = ur.tenant_id AND rp.role_id = ur.role_id LEFT JOIN modura.role_policy_departments rpd ON rpd.tenant_id = rp.tenant_id AND rpd.role_id = rp.role_id AND rpd.resource = rp.resource AND rpd.action = rp.action WHERE ur.tenant_id = $1 AND ur.user_id = $2 GROUP BY ur.role_id, rp.resource, rp.action, rp.data_scope ORDER BY ur.role_id, rp.resource, rp.action`, actor.TenantID, actor.UserID)
	if err != nil {
		return nil, fmt.Errorf("query role policies: %w", err)
	}
	defer rows.Close()
	byRole := make(map[authorization.RoleID]*authorization.RolePolicies)
	var ordered []authorization.RoleID
	for rows.Next() {
		var roleID authorization.RoleID
		var policy authorization.Policy
		if err := rows.Scan(&roleID, &policy.Resource, &policy.Action, &policy.Scope, &policy.DepartmentIDs); err != nil {
			return nil, fmt.Errorf("scan role policy: %w", err)
		}
		role := byRole[roleID]
		if role == nil {
			role = &authorization.RolePolicies{RoleID: roleID}
			byRole[roleID] = role
			ordered = append(ordered, roleID)
		}
		role.Policies = append(role.Policies, policy)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate role policies: %w", err)
	}
	result := make([]authorization.RolePolicies, 0, len(ordered))
	for _, roleID := range ordered {
		result = append(result, *byRole[roleID])
	}
	return result, nil
}

func tenantAdministratorPermissions() []authorization.Permission {
	var permissions []authorization.Permission
	for _, item := range []struct {
		resource authorization.Resource
		actions  []authorization.Action
	}{
		{authorization.ResourceUsers, []authorization.Action{authorization.ActionRead, authorization.ActionUpdate}},
		{authorization.ResourceDepartments, []authorization.Action{authorization.ActionRead, authorization.ActionCreate, authorization.ActionUpdate, authorization.ActionDelete}},
		{authorization.ResourcePositions, []authorization.Action{authorization.ActionRead, authorization.ActionCreate, authorization.ActionUpdate}},
		{authorization.ResourceUserOrganization, []authorization.Action{authorization.ActionRead, authorization.ActionUpdate}},
		{authorization.ResourceRoles, []authorization.Action{authorization.ActionRead, authorization.ActionCreate, authorization.ActionUpdate, authorization.ActionDelete}},
		{authorization.ResourcePolicies, []authorization.Action{authorization.ActionRead, authorization.ActionUpdate}},
		{authorization.ResourceUserRoles, []authorization.Action{authorization.ActionRead, authorization.ActionUpdate}},
		{authorization.ResourceDictionaries, []authorization.Action{authorization.ActionRead, authorization.ActionCreate, authorization.ActionUpdate, authorization.ActionDelete}},
		{authorization.ResourceConfigurations, []authorization.Action{authorization.ActionRead, authorization.ActionUpdate}},
		{authorization.ResourceAuditEvents, []authorization.Action{authorization.ActionRead}},
	} {
		for _, action := range item.actions {
			permissions = append(permissions, authorization.Permission{Resource: item.resource, Action: action})
		}
	}
	return permissions
}
