// Package postgres persists organization-owned data in PostgreSQL.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/psmMRFP/WhereToLive/backend/internal/modules/identity"
	"github.com/psmMRFP/WhereToLive/backend/internal/modules/organization"
)

// Store persists tenant-scoped organization data.
type Store struct{ pool *pgxpool.Pool }

// New constructs a PostgreSQL organization store.
func New(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

// ListDepartments returns only rows explicitly scoped to the supplied tenant.
func (s *Store) ListDepartments(ctx context.Context, tenantID identity.TenantID, scope organization.DataScope) ([]organization.DepartmentView, error) {
	customIDs := make([]string, 0, len(scope.CustomDepartmentIDs))
	for _, id := range scope.CustomDepartmentIDs {
		customIDs = append(customIDs, string(id))
	}
	const query = `
WITH RECURSIVE actor_department AS (
    SELECT primary_department_id AS id
    FROM wheretolive.user_organization
    WHERE tenant_id = $1 AND user_id = $3
), descendants AS (
    SELECT id FROM actor_department WHERE $5
    UNION ALL
    SELECT d.id
    FROM wheretolive.departments d
    JOIN descendants parent ON d.parent_id = parent.id
    WHERE d.tenant_id = $1
)
SELECT d.id, d.parent_id, d.name, d.sort_order
FROM wheretolive.departments d
WHERE d.tenant_id = $1 AND (
    $2
    OR (($4 OR $6) AND d.id IN (SELECT id FROM actor_department))
    OR ($5 AND d.id IN (SELECT id FROM descendants))
    OR d.id = ANY($7::uuid[])
)
ORDER BY d.sort_order, d.normalized_name, d.id`
	rows, err := s.pool.Query(ctx, query, tenantID, scope.All, scope.ActorID, scope.Department, scope.DepartmentAndDescendants, scope.Self, customIDs)
	if err != nil {
		return nil, fmt.Errorf("query departments: %w", err)
	}
	defer rows.Close()
	departments := make([]organization.DepartmentView, 0)
	for rows.Next() {
		var department organization.DepartmentView
		if err := rows.Scan(&department.ID, &department.ParentID, &department.Name, &department.SortOrder); err != nil {
			return nil, fmt.Errorf("scan department: %w", err)
		}
		departments = append(departments, department)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate departments: %w", err)
	}
	return departments, nil
}

// ListPositions returns only positions explicitly scoped to the supplied tenant.
func (s *Store) ListPositions(ctx context.Context, tenantID identity.TenantID) ([]organization.PositionView, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, name, status FROM wheretolive.positions WHERE tenant_id = $1 ORDER BY normalized_name, id`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("query positions: %w", err)
	}
	defer rows.Close()
	positions := make([]organization.PositionView, 0)
	for rows.Next() {
		var position organization.PositionView
		if err := rows.Scan(&position.ID, &position.Name, &position.Status); err != nil {
			return nil, fmt.Errorf("scan position: %w", err)
		}
		positions = append(positions, position)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate positions: %w", err)
	}
	return positions, nil
}

// CreateDepartment inserts a department and relies on composite foreign keys
// for same-tenant parent ownership.
func (s *Store) CreateDepartment(ctx context.Context, tx pgx.Tx, department organization.Department, scope organization.DataScope) error {
	if department.ParentID != nil && !visibleDepartment(ctx, tx, department.TenantID, *department.ParentID, scope) {
		return organization.ErrNotFound
	}
	_, err := tx.Exec(ctx, `
INSERT INTO wheretolive.departments
    (id, tenant_id, parent_id, name, normalized_name, sort_order, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $7)`, department.ID, department.TenantID, department.ParentID, department.Name, department.NormalizedName, department.SortOrder, department.CreatedAt)
	if err != nil {
		return fmt.Errorf("insert department: %w", err)
	}
	return nil
}

// UpdateDepartment updates display fields after enforcing tenant and data scope.
func (s *Store) UpdateDepartment(ctx context.Context, tx pgx.Tx, tenantID identity.TenantID, departmentID organization.DepartmentID, name, normalizedName string, sortOrder int, scope organization.DataScope, now time.Time) error {
	if !visibleDepartment(ctx, tx, tenantID, departmentID, scope) {
		return organization.ErrNotFound
	}
	command, err := tx.Exec(ctx, `UPDATE wheretolive.departments SET name = $3, normalized_name = $4, sort_order = $5, updated_at = $6 WHERE tenant_id = $1 AND id = $2`, tenantID, departmentID, name, normalizedName, sortOrder, now)
	if err != nil {
		return fmt.Errorf("update department: %w", err)
	}
	if command.RowsAffected() != 1 {
		return organization.ErrNotFound
	}
	return nil
}

// MoveDepartment atomically rejects root moves, cross-tenant parents, and cycles.
func (s *Store) MoveDepartment(ctx context.Context, tx pgx.Tx, tenantID identity.TenantID, departmentID, newParentID organization.DepartmentID, scope organization.DataScope, now time.Time) error {
	if !visibleDepartment(ctx, tx, tenantID, departmentID, scope) || !visibleDepartment(ctx, tx, tenantID, newParentID, scope) {
		return organization.ErrNotFound
	}
	var isRoot bool
	if err := tx.QueryRow(ctx, `SELECT parent_id IS NULL FROM wheretolive.departments WHERE tenant_id = $1 AND id = $2 FOR UPDATE`, tenantID, departmentID).Scan(&isRoot); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return organization.ErrNotFound
		}
		return fmt.Errorf("lock department: %w", err)
	}
	if isRoot {
		return organization.ErrRootDepartment
	}
	var parentExists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM wheretolive.departments WHERE tenant_id = $1 AND id = $2)`, tenantID, newParentID).Scan(&parentExists); err != nil {
		return fmt.Errorf("check department parent: %w", err)
	}
	if !parentExists {
		return organization.ErrNotFound
	}
	const cycleQuery = `
WITH RECURSIVE descendants AS (
    SELECT id FROM wheretolive.departments WHERE tenant_id = $1 AND id = $2
    UNION ALL
    SELECT d.id FROM wheretolive.departments d
    JOIN descendants parent ON d.parent_id = parent.id
    WHERE d.tenant_id = $1
)
SELECT EXISTS (SELECT 1 FROM descendants WHERE id = $3)`
	var cycle bool
	if err := tx.QueryRow(ctx, cycleQuery, tenantID, departmentID, newParentID).Scan(&cycle); err != nil {
		return fmt.Errorf("check department cycle: %w", err)
	}
	if cycle {
		return organization.ErrCycle
	}
	if _, err := tx.Exec(ctx, `UPDATE wheretolive.departments SET parent_id = $3, updated_at = $4 WHERE tenant_id = $1 AND id = $2`, tenantID, departmentID, newParentID, now); err != nil {
		return fmt.Errorf("move department: %w", err)
	}
	return nil
}

// DeleteDepartment deletes an unused non-root department.
func (s *Store) DeleteDepartment(ctx context.Context, tx pgx.Tx, tenantID identity.TenantID, departmentID organization.DepartmentID, scope organization.DataScope) error {
	if !visibleDepartment(ctx, tx, tenantID, departmentID, scope) {
		return organization.ErrNotFound
	}
	command, err := tx.Exec(ctx, `DELETE FROM wheretolive.departments WHERE tenant_id = $1 AND id = $2 AND parent_id IS NOT NULL`, tenantID, departmentID)
	if err != nil {
		var postgresError *pgconn.PgError
		if errors.As(err, &postgresError) && postgresError.Code == "23503" {
			return organization.ErrInUse
		}
		return fmt.Errorf("delete department: %w", err)
	}
	if command.RowsAffected() != 1 {
		var isRoot bool
		err := tx.QueryRow(ctx, `SELECT parent_id IS NULL FROM wheretolive.departments WHERE tenant_id = $1 AND id = $2`, tenantID, departmentID).Scan(&isRoot)
		if errors.Is(err, pgx.ErrNoRows) {
			return organization.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("check deleted department: %w", err)
		}
		if isRoot {
			return organization.ErrRootDepartment
		}
	}
	return nil
}

// CreatePosition inserts an active tenant position.
func (s *Store) CreatePosition(ctx context.Context, tx pgx.Tx, position organization.Position) error {
	_, err := tx.Exec(ctx, `
INSERT INTO wheretolive.positions (id, tenant_id, name, normalized_name, status, created_at, updated_at)
VALUES ($1, $2, $3, $4, 'active', $5, $5)`, position.ID, position.TenantID, position.Name, position.NormalizedName, position.CreatedAt)
	if err != nil {
		return fmt.Errorf("insert position: %w", err)
	}
	return nil
}

// UpdatePosition updates a tenant position's display fields and status.
func (s *Store) UpdatePosition(ctx context.Context, tx pgx.Tx, tenantID identity.TenantID, positionID organization.PositionID, name, normalizedName string, status organization.PositionStatus, now time.Time) error {
	command, err := tx.Exec(ctx, `UPDATE wheretolive.positions SET name = $3, normalized_name = $4, status = $5, updated_at = $6 WHERE tenant_id = $1 AND id = $2`, tenantID, positionID, name, normalizedName, status, now)
	if err != nil {
		return fmt.Errorf("update position: %w", err)
	}
	if command.RowsAffected() != 1 {
		return organization.ErrNotFound
	}
	return nil
}

// AssignUser upserts the single primary department and optional position.
func (s *Store) AssignUser(ctx context.Context, tx pgx.Tx, tenantID identity.TenantID, userID identity.UserID, departmentID organization.DepartmentID, positionID *organization.PositionID, scope organization.DataScope, now time.Time) error {
	if !visibleUser(ctx, tx, tenantID, userID, scope) || !visibleDepartment(ctx, tx, tenantID, departmentID, scope) {
		return organization.ErrNotFound
	}
	_, err := tx.Exec(ctx, `
INSERT INTO wheretolive.user_organization
    (tenant_id, user_id, primary_department_id, position_id, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $5)
ON CONFLICT (tenant_id, user_id) DO UPDATE
SET primary_department_id = EXCLUDED.primary_department_id,
    position_id = EXCLUDED.position_id,
    updated_at = EXCLUDED.updated_at`, tenantID, userID, departmentID, positionID, now)
	if err != nil {
		return fmt.Errorf("assign user organization: %w", err)
	}
	return nil
}

func visibleDepartment(ctx context.Context, tx pgx.Tx, tenantID identity.TenantID, departmentID organization.DepartmentID, scope organization.DataScope) bool {
	const query = `WITH RECURSIVE actor_department AS (SELECT primary_department_id AS id FROM wheretolive.user_organization WHERE tenant_id = $1 AND user_id = $3), descendants AS (SELECT id FROM actor_department WHERE $6 UNION ALL SELECT d.id FROM wheretolive.departments d JOIN descendants parent ON d.parent_id = parent.id WHERE d.tenant_id = $1) SELECT EXISTS (SELECT 1 FROM wheretolive.departments d WHERE d.tenant_id = $1 AND d.id = $2 AND ($4 OR (($5 OR $7) AND d.id IN (SELECT id FROM actor_department)) OR ($6 AND d.id IN (SELECT id FROM descendants)) OR d.id = ANY($8::uuid[])))`
	custom := make([]string, 0, len(scope.CustomDepartmentIDs))
	for _, id := range scope.CustomDepartmentIDs {
		custom = append(custom, string(id))
	}
	var visible bool
	if err := tx.QueryRow(ctx, query, tenantID, departmentID, scope.ActorID, scope.All, scope.Department, scope.DepartmentAndDescendants, scope.Self, custom).Scan(&visible); err != nil {
		return false
	}
	return visible
}

func visibleUser(ctx context.Context, tx pgx.Tx, tenantID identity.TenantID, userID identity.UserID, scope organization.DataScope) bool {
	if scope.All || (scope.Self && userID == scope.ActorID) {
		return true
	}
	const query = `WITH RECURSIVE actor_department AS (SELECT primary_department_id AS id FROM wheretolive.user_organization WHERE tenant_id = $1 AND user_id = $3), descendants AS (SELECT id FROM actor_department WHERE $6 UNION ALL SELECT d.id FROM wheretolive.departments d JOIN descendants parent ON d.parent_id = parent.id WHERE d.tenant_id = $1) SELECT EXISTS (SELECT 1 FROM wheretolive.user_organization target WHERE target.tenant_id = $1 AND target.user_id = $2 AND ((($4) AND target.primary_department_id IN (SELECT id FROM actor_department)) OR ($6 AND target.primary_department_id IN (SELECT id FROM descendants)) OR target.primary_department_id = ANY($7::uuid[])))`
	custom := make([]string, 0, len(scope.CustomDepartmentIDs))
	for _, id := range scope.CustomDepartmentIDs {
		custom = append(custom, string(id))
	}
	var visible bool
	if err := tx.QueryRow(ctx, query, tenantID, userID, scope.ActorID, scope.Department, scope.Self, scope.DepartmentAndDescendants, custom).Scan(&visible); err != nil {
		return false
	}
	return visible
}

// ProvisionInitialOrganization creates the root and first assignment in a workflow transaction.
func (s *Store) ProvisionInitialOrganization(ctx context.Context, tx pgx.Tx, root organization.Department, administratorID identity.UserID) error {
	if _, err := tx.Exec(ctx, `
INSERT INTO wheretolive.departments
    (id, tenant_id, parent_id, name, normalized_name, sort_order, created_at, updated_at)
VALUES ($1, $2, NULL, $3, $4, $5, $6, $6)`, root.ID, root.TenantID, root.Name, root.NormalizedName, root.SortOrder, root.CreatedAt); err != nil {
		return fmt.Errorf("insert root department: %w", err)
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO wheretolive.user_organization
    (tenant_id, user_id, primary_department_id, created_at, updated_at)
VALUES ($1, $2, $3, $4, $4)`, root.TenantID, administratorID, root.ID, root.CreatedAt); err != nil {
		return fmt.Errorf("assign initial administrator organization: %w", err)
	}
	return nil
}
