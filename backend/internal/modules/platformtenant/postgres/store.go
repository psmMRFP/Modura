// Package postgres persists platform-level tenant lifecycle operations.
package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/modura-dev/modura/backend/internal/modules/platformtenant"
)

// Store persists tenant lifecycle state and its audit evidence.
type Store struct{ pool *pgxpool.Pool }

// New constructs a PostgreSQL platform tenant store.
func New(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

// List returns platform-visible tenant summaries in stable order.
func (s *Store) List(ctx context.Context) ([]platformtenant.Tenant, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, slug, display_name, status, created_at, updated_at FROM modura.tenants ORDER BY created_at, id`)
	if err != nil {
		return nil, fmt.Errorf("list tenants: %w", err)
	}
	defer rows.Close()
	tenants := make([]platformtenant.Tenant, 0)
	for rows.Next() {
		var tenant platformtenant.Tenant
		if err := rows.Scan(&tenant.ID, &tenant.Slug, &tenant.DisplayName, &tenant.Status, &tenant.CreatedAt, &tenant.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan tenant: %w", err)
		}
		tenants = append(tenants, tenant)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate tenants: %w", err)
	}
	return tenants, nil
}

// UpdateProfile atomically applies an optimistic update and records snapshots.
func (s *Store) UpdateProfile(ctx context.Context, change platformtenant.ProfileChange) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin tenant profile update: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var slug, displayName, status string
	var updatedAt time.Time
	err = tx.QueryRow(ctx, `SELECT slug, display_name, status, updated_at FROM modura.tenants WHERE id = $1 FOR UPDATE`, change.TenantID).Scan(&slug, &displayName, &status, &updatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return platformtenant.ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("read tenant profile: %w", err)
	}
	if !updatedAt.Equal(change.ExpectedUpdatedAt) {
		return platformtenant.ErrConflict
	}
	before, err := json.Marshal(map[string]any{"slug": slug, "displayName": displayName, "status": status})
	if err != nil {
		return fmt.Errorf("encode previous tenant profile: %w", err)
	}
	after, err := json.Marshal(map[string]any{"slug": slug, "displayName": change.DisplayName, "status": status})
	if err != nil {
		return fmt.Errorf("encode updated tenant profile: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE modura.tenants SET display_name = $2, updated_at = $3 WHERE id = $1`, change.TenantID, change.DisplayName, change.OccurredAt); err != nil {
		return fmt.Errorf("update tenant profile: %w", err)
	}
	_, err = tx.Exec(ctx, `INSERT INTO modura.audit_events (id, actor_type, actor_id, tenant_id, action, resource, resource_id, reason, result, correlation_id, occurred_at, before_state, after_state) VALUES ($1, 'platform_administrator', $2, $3, 'tenant.profile-updated', 'tenant', $3, $4, 'succeeded', $5, $6, $7::jsonb, $8::jsonb)`, change.AuditID, change.Actor.AdministratorID, change.TenantID, change.Reason, change.CorrelationID, change.OccurredAt, before, after)
	if err != nil {
		return fmt.Errorf("record tenant profile audit: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tenant profile update: %w", err)
	}
	return nil
}

// ChangeStatus atomically updates tenant state and records audit evidence.
func (s *Store) ChangeStatus(ctx context.Context, change platformtenant.LifecycleChange, from, to string) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin tenant lifecycle change: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	command, err := tx.Exec(ctx, `UPDATE modura.tenants SET status = $3, updated_at = $4 WHERE id = $1 AND status = $2`, change.TenantID, from, to, change.OccurredAt)
	if err != nil {
		return fmt.Errorf("update tenant lifecycle: %w", err)
	}
	if command.RowsAffected() != 1 {
		var status string
		err := tx.QueryRow(ctx, `SELECT status FROM modura.tenants WHERE id = $1`, change.TenantID).Scan(&status)
		if errors.Is(err, pgx.ErrNoRows) {
			return platformtenant.ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("read tenant lifecycle: %w", err)
		}
		return platformtenant.ErrInvalidTransition
	}
	action := "tenant." + to
	_, err = tx.Exec(ctx, `INSERT INTO modura.audit_events (id, actor_type, actor_id, tenant_id, action, resource, resource_id, reason, result, correlation_id, occurred_at) VALUES ($1, 'platform_administrator', $2, $3, $4, 'tenant', $3, $5, 'succeeded', $6, $7)`, change.AuditID, change.Actor.AdministratorID, change.TenantID, action, change.Reason, change.CorrelationID, change.OccurredAt)
	if err != nil {
		return fmt.Errorf("record tenant lifecycle audit: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tenant lifecycle change: %w", err)
	}
	return nil
}
