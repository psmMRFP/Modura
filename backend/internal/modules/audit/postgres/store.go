// Package postgres persists audit-owned records in PostgreSQL.
package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/modura-dev/modura/backend/internal/modules/audit"
	auditdb "github.com/modura-dev/modura/backend/internal/modules/audit/postgres/db"
	"github.com/modura-dev/modura/backend/internal/modules/identity"
)

// Store persists immutable audit evidence.
type Store struct{ pool *pgxpool.Pool }

// New constructs an audit store.
func New(pools ...*pgxpool.Pool) Store {
	var pool *pgxpool.Pool
	if len(pools) > 0 {
		pool = pools[0]
	}
	return Store{pool: pool}
}

// Record inserts a successful tenant-user audit event in the caller transaction.
func (Store) Record(ctx context.Context, tx pgx.Tx, event audit.Event) error {
	if err := auditdb.New(tx).InsertTenantAuditEvent(ctx, auditdb.InsertTenantAuditEventParams{ID: event.ID, ActorID: string(event.ActorID), TenantID: pgUUID(string(event.TenantID)), Action: event.Action, Resource: event.Resource, ResourceID: event.ResourceID, Reason: event.Reason, CorrelationID: event.CorrelationID, OccurredAt: event.OccurredAt, BeforeState: nullableJSON(event.BeforeState), AfterState: nullableJSON(event.AfterState)}); err != nil {
		return fmt.Errorf("insert audit event: %w", err)
	}
	return nil
}

// RecordPlatform inserts a successful global-administrator audit event.
func (Store) RecordPlatform(ctx context.Context, tx pgx.Tx, event audit.PlatformEvent) error {
	if err := auditdb.New(tx).InsertPlatformAuditEvent(ctx, auditdb.InsertPlatformAuditEventParams{ID: event.ID, ActorID: event.ActorID, Action: event.Action, Resource: event.Resource, ResourceID: event.ResourceID, Reason: event.Reason, CorrelationID: event.CorrelationID, OccurredAt: event.OccurredAt, BeforeState: nullableJSON(event.BeforeState), AfterState: nullableJSON(event.AfterState)}); err != nil {
		return fmt.Errorf("insert platform audit event: %w", err)
	}
	return nil
}

// RecordTenantScopedPlatform inserts a platform-administrator audit event for
// exactly one existing tenant in the caller transaction.
func (Store) RecordTenantScopedPlatform(ctx context.Context, tx pgx.Tx, event audit.TenantScopedPlatformEvent) error {
	if err := auditdb.New(tx).InsertTenantScopedPlatformAuditEvent(ctx, auditdb.InsertTenantScopedPlatformAuditEventParams{ID: event.ID, ActorID: event.ActorID, TenantID: pgUUID(string(event.TenantID)), Action: event.Action, Resource: event.Resource, ResourceID: event.ResourceID, Reason: event.Reason, CorrelationID: event.CorrelationID, OccurredAt: event.OccurredAt, BeforeState: nullableJSON(event.BeforeState), AfterState: nullableJSON(event.AfterState)}); err != nil {
		return fmt.Errorf("insert tenant-scoped platform audit event: %w", err)
	}
	return nil
}

// pgUUID converts the application's string identifier to the generated UUID type.
func pgUUID(value string) pgtype.UUID {
	parsed, err := uuid.Parse(value)
	if err != nil {
		return pgtype.UUID{}
	}
	return pgtype.UUID{Bytes: parsed, Valid: true}
}

// List returns only immutable events in the explicitly supplied tenant.
func (s Store) List(ctx context.Context, query audit.Query) ([]audit.Record, error) {
	if s.pool == nil {
		return nil, fmt.Errorf("audit query store is unavailable")
	}
	rows, err := s.pool.Query(ctx, `SELECT `+recordColumns+` FROM modura.audit_events WHERE tenant_id = $1 AND ($2 = '' OR action = $2) AND ($3 = '' OR resource = $3) ORDER BY occurred_at DESC, id DESC LIMIT $4 OFFSET $5`, query.TenantID, query.Action, query.Resource, query.Limit, query.Offset)
	if err != nil {
		return nil, fmt.Errorf("query audit events: %w", err)
	}
	return collectRecords(rows)
}

// Get returns one immutable event only within the explicitly supplied tenant.
func (s Store) Get(ctx context.Context, tenantID identity.TenantID, eventID string) (audit.Record, error) {
	if s.pool == nil {
		return audit.Record{}, fmt.Errorf("audit query store is unavailable")
	}
	var record audit.Record
	err := s.pool.QueryRow(ctx, `SELECT `+recordColumns+` FROM modura.audit_events WHERE id = $1 AND tenant_id = $2`, eventID, tenantID).Scan(&record.ID, &record.ActorType, &record.ActorID, &record.TenantID, &record.Action, &record.Resource, &record.ResourceID, &record.Reason, &record.Result, &record.CorrelationID, &record.OccurredAt, &record.BeforeState, &record.AfterState)
	if errors.Is(err, pgx.ErrNoRows) {
		return audit.Record{}, audit.ErrNotFound
	}
	if err != nil {
		return audit.Record{}, fmt.Errorf("get audit event: %w", err)
	}
	return record, nil
}

// ListPlatform returns immutable events across tenants; platform events carry
// no tenant and only appear when the tenant filter is empty.
func (s Store) ListPlatform(ctx context.Context, query audit.PlatformQuery) ([]audit.Record, error) {
	if s.pool == nil {
		return nil, fmt.Errorf("audit query store is unavailable")
	}
	var tenantFilter any
	if query.TenantID != "" {
		tenantFilter = query.TenantID
	}
	rows, err := s.pool.Query(ctx, `SELECT `+recordColumns+` FROM modura.audit_events WHERE ($1::uuid IS NULL OR tenant_id = $1::uuid) AND ($2 = '' OR action = $2) AND ($3 = '' OR resource = $3) ORDER BY occurred_at DESC, id DESC LIMIT $4 OFFSET $5`, tenantFilter, query.Action, query.Resource, query.Limit, query.Offset)
	if err != nil {
		return nil, fmt.Errorf("query platform audit events: %w", err)
	}
	return collectRecords(rows)
}

// recordColumns is the stable immutable projection of an audit event.
const recordColumns = `id, actor_type, actor_id, tenant_id, action, resource, resource_id, reason, result, correlation_id, occurred_at, before_state, after_state`

func collectRecords(rows pgx.Rows) ([]audit.Record, error) {
	defer rows.Close()
	records := make([]audit.Record, 0)
	for rows.Next() {
		var record audit.Record
		var tenantID *string
		if err := rows.Scan(&record.ID, &record.ActorType, &record.ActorID, &tenantID, &record.Action, &record.Resource, &record.ResourceID, &record.Reason, &record.Result, &record.CorrelationID, &record.OccurredAt, &record.BeforeState, &record.AfterState); err != nil {
			return nil, fmt.Errorf("scan audit event: %w", err)
		}
		if tenantID != nil {
			record.TenantID = identity.TenantID(*tenantID)
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

func nullableJSON(value []byte) []byte {
	if len(value) == 0 {
		return nil
	}
	return value
}
