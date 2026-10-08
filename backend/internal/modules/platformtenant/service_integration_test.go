package platformtenant_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/modura-dev/modura/backend/internal/modules/audit"
	auditpostgres "github.com/modura-dev/modura/backend/internal/modules/audit/postgres"
	"github.com/modura-dev/modura/backend/internal/modules/identity"
	identitypostgres "github.com/modura-dev/modura/backend/internal/modules/identity/postgres"
	"github.com/modura-dev/modura/backend/internal/modules/platformadmin"
	"github.com/modura-dev/modura/backend/internal/modules/platformtenant"
	"github.com/modura-dev/modura/backend/internal/platform/database"
	"github.com/modura-dev/modura/backend/internal/platform/database/migrationtest"
)

func TestTenantLifecycleAndAuditAreAtomic(t *testing.T) {
	pool := integrationPool(t)
	ctx := context.Background()
	now := time.Unix(1_700_000_000, 0).UTC()
	sequence := 0
	if _, err := pool.Exec(ctx, `INSERT INTO modura.platform_administrators (id, username, normalized_username, password_hash, status, created_at, updated_at) VALUES ($1, 'operator', 'operator', 'hash', 'active', $2, $2)`, "018bcfe5-6800-7000-8000-000000000901", now); err != nil {
		t.Fatal(err)
	}
	newID := func(time.Time) (string, error) {
		sequence++
		return fmt.Sprintf("018bcfe5-6800-7000-8000-%012d", 910+sequence), nil
	}
	auditService, err := audit.NewService(auditpostgres.New(pool), newID)
	if err != nil {
		t.Fatal(err)
	}
	if err := auditService.EnableQueries(auditpostgres.New(pool)); err != nil {
		t.Fatal(err)
	}
	key := []byte(strings.Repeat("k", 32))
	signer, err := identity.NewAccessTokenSigner("modura", "admin", "key-1", key, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	identityService, err := identity.NewService(identitypostgres.New(pool), signer, identity.NewAccessTokenVerifier("modura", "admin", map[string][]byte{"key-1": key}, 0), identity.DefaultPasswordParameters(), time.Hour, func() time.Time { return now }, newID, func() (string, error) { return strings.Repeat("s", 32), nil })
	if err != nil {
		t.Fatal(err)
	}
	if err := identityService.EnableUserManagement(database.NewTransactor(pool), workflowAuditor{service: auditService}); err != nil {
		t.Fatal(err)
	}
	service, err := platformtenant.NewService(identityService, database.NewTransactor(pool), auditService, func() time.Time { return now }, newID)
	if err != nil {
		t.Fatal(err)
	}
	tenantID := identity.TenantID("018bcfe5-6800-7000-8000-000000000902")
	if _, err := pool.Exec(ctx, `INSERT INTO modura.tenants (id, slug, display_name, status, created_at, updated_at) VALUES ($1, 'acme', 'Acme', 'active', $2, $2)`, tenantID, now); err != nil {
		t.Fatal(err)
	}
	actor := platformadmin.Actor{AdministratorID: "018bcfe5-6800-7000-8000-000000000901", SessionID: "018bcfe5-6800-7000-8000-000000000903"}

	tenants, err := service.List(ctx, actor)
	if err != nil || len(tenants) != 1 || tenants[0].DisplayName != "Acme" {
		t.Fatalf("list err=%v tenants=%+v", err, tenants)
	}
	if err := service.UpdateProfile(ctx, actor, tenantID, "Acme Updated", now, "customer request", "request-profile"); err != nil {
		t.Fatal(err)
	}
	var displayName, action string
	var snapshotsPresent bool
	if err := pool.QueryRow(ctx, `SELECT display_name FROM modura.tenants WHERE id = $1`, tenantID).Scan(&displayName); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT action, before_state IS NOT NULL AND after_state IS NOT NULL FROM modura.audit_events WHERE tenant_id = $1 ORDER BY occurred_at DESC LIMIT 1`, tenantID).Scan(&action, &snapshotsPresent); err != nil {
		t.Fatal(err)
	}
	if displayName != "Acme Updated" || action != "tenant.profile-updated" || !snapshotsPresent {
		t.Fatalf("profile=%q action=%q snapshots=%t", displayName, action, snapshotsPresent)
	}
	if err := service.UpdateProfile(ctx, actor, tenantID, "Stale", now.Add(-time.Second), "stale edit", "request-stale"); !errors.Is(err, platformtenant.ErrConflict) {
		t.Fatalf("stale profile update error = %v", err)
	}
	if err := service.Suspend(ctx, actor, tenantID, "security review", "request-1"); err != nil {
		t.Fatal(err)
	}
	assertStatusAndAuditCount(t, pool, tenantID, "suspended", 2)
	if err := service.Suspend(ctx, actor, tenantID, "duplicate", "request-2"); !errors.Is(err, platformtenant.ErrInvalidTransition) {
		t.Fatalf("duplicate suspension error = %v", err)
	}
	assertStatusAndAuditCount(t, pool, tenantID, "suspended", 2)
	if err := service.Reactivate(ctx, actor, tenantID, "review complete", "request-3"); err != nil {
		t.Fatal(err)
	}
	assertStatusAndAuditCount(t, pool, tenantID, "active", 3)
}

func assertStatusAndAuditCount(t *testing.T, pool *pgxpool.Pool, tenantID identity.TenantID, wantStatus string, wantCount int) {
	t.Helper()
	var status string
	var count int
	if err := pool.QueryRow(context.Background(), `SELECT status FROM modura.tenants WHERE id = $1`, tenantID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM modura.audit_events WHERE tenant_id = $1`, tenantID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if status != wantStatus || count != wantCount {
		t.Fatalf("status=%s count=%d, want status=%s count=%d", status, count, wantStatus, wantCount)
	}
}

// workflowAuditor adapts the audit service to the identity account audit contract.
type workflowAuditor struct {
	service *audit.Service
}

func (a workflowAuditor) RecordAccountEvent(ctx context.Context, tx pgx.Tx, event identity.AccountAuditEvent) error {
	return a.service.RecordTenantWrite(ctx, tx, audit.Event{ActorID: event.Actor.UserID, TenantID: event.Actor.TenantID, Action: event.Action, Resource: event.Resource, ResourceID: event.ResourceID, Reason: event.Reason, CorrelationID: event.CorrelationID, OccurredAt: event.OccurredAt, BeforeState: event.BeforeState, AfterState: event.AfterState})
}

func integrationPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("MODURA_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("MODURA_TEST_DATABASE_URL is not set")
	}
	config, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(config.ConnConfig.Database, "_test") {
		t.Fatalf("refusing destructive integration setup for database %q: name must end in _test", config.ConnConfig.Database)
	}
	pool, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	migrationtest.Prepare(t, pool)
	return pool
}
