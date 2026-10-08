package postgres

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/modura-dev/modura/backend/internal/modules/audit"
	"github.com/modura-dev/modura/backend/internal/modules/identity"
	"github.com/modura-dev/modura/backend/internal/platform/database/migrationtest"
)

func TestAuditQueriesAreTenantScopedAndFiltered(t *testing.T) {
	pool := integrationPool(t)
	ctx := context.Background()
	now := time.Unix(1_700_000_000, 0).UTC()
	if _, err := pool.Exec(ctx, `INSERT INTO modura.tenants (id, slug, display_name, status, created_at, updated_at) VALUES ('018bcfe5-6800-7000-8000-000000000401', 'delta', 'Delta', 'active', $1, $1)`, now); err != nil {
		t.Fatal(err)
	}
	store := New(pool)
	sequence := 0
	auditService, err := audit.NewService(store, func(time.Time) (string, error) {
		sequence++
		return fmt.Sprintf("018bcfe5-6800-7000-8000-%012d", 430+sequence), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := auditService.EnableQueries(store); err != nil {
		t.Fatal(err)
	}
	tenantID := identity.TenantID("018bcfe5-6800-7000-8000-000000000401")
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tenantEvent := audit.Event{ActorID: "018bcfe5-6800-7000-8000-000000000411", TenantID: tenantID, Action: "identity.user.disabled", Resource: "user", ResourceID: "018bcfe5-6800-7000-8000-000000000412", Reason: "policy violation", CorrelationID: "request-audit-tenant", OccurredAt: now, BeforeState: []byte(`{"status":"active"}`), AfterState: []byte(`{"status":"disabled","refreshToken":"secret"}`)}
	if err := auditService.RecordTenantWrite(ctx, tx, tenantEvent); err != nil {
		t.Fatal(err)
	}
	platformEvent := audit.PlatformEvent{ActorID: "018bcfe5-6800-7000-8000-000000000421", Action: "tenant.provisioned", Resource: "tenant", ResourceID: "018bcfe5-6800-7000-8000-000000000401", Reason: "customer onboarding", CorrelationID: "request-audit-platform", OccurredAt: now.Add(time.Second)}
	if err := auditService.RecordPlatformWrite(ctx, tx, platformEvent); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	records, err := auditService.List(ctx, tenantID, "", "", 50, 0)
	if err != nil || len(records) != 1 {
		t.Fatalf("tenant list err=%v records=%d", err, len(records))
	}
	if strings.Contains(string(records[0].AfterState), "secret") {
		t.Fatalf("tenant list leaked sensitive state: %s", records[0].AfterState)
	}
	record, err := auditService.Get(ctx, tenantID, "018bcfe5-6800-7000-8000-000000000431")
	if err != nil {
		t.Fatal(err)
	}
	if record.Action != "identity.user.disabled" || strings.Contains(string(record.AfterState), "secret") {
		t.Fatalf("tenant detail action=%q state=%s", record.Action, record.AfterState)
	}
	if _, err := auditService.Get(ctx, tenantID, "018bcfe5-6800-7000-8000-000000000999"); err == nil {
		t.Fatal("lookup outside tenant succeeded")
	}
	all, err := auditService.ListPlatform(ctx, audit.PlatformQuery{Limit: 50})
	if err != nil || len(all) != 2 {
		t.Fatalf("platform list err=%v records=%d", err, len(all))
	}
	filtered, err := auditService.ListPlatform(ctx, audit.PlatformQuery{TenantID: tenantID, Limit: 50})
	if err != nil || len(filtered) != 1 || filtered[0].Action != "identity.user.disabled" {
		t.Fatalf("platform tenant filter err=%v records=%+v", err, filtered)
	}
	platformOnly, err := auditService.ListPlatform(ctx, audit.PlatformQuery{Action: "tenant.provisioned", Limit: 50})
	if err != nil || len(platformOnly) != 1 || platformOnly[0].TenantID != "" {
		t.Fatalf("platform action filter err=%v records=%+v", err, platformOnly)
	}
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
