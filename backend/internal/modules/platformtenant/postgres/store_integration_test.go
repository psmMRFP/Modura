package postgres

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/modura-dev/modura/backend/internal/modules/identity"
	"github.com/modura-dev/modura/backend/internal/modules/platformadmin"
	"github.com/modura-dev/modura/backend/internal/modules/platformtenant"
)

func TestTenantLifecycleAndAuditAreAtomic(t *testing.T) {
	pool := integrationPool(t)
	ctx := context.Background()
	now := time.Unix(1_700_000_000, 0).UTC()
	administratorID := "018bcfe5-6800-7000-8000-000000000901"
	tenantID := identity.TenantID("018bcfe5-6800-7000-8000-000000000902")
	if _, err := pool.Exec(ctx, `INSERT INTO modura.platform_administrators (id, username, normalized_username, password_hash, status, created_at, updated_at) VALUES ($1, 'operator', 'operator', 'hash', 'active', $2, $2)`, administratorID, now); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO modura.tenants (id, slug, display_name, status, created_at, updated_at) VALUES ($1, 'acme', 'Acme', 'active', $2, $2)`, tenantID, now); err != nil {
		t.Fatal(err)
	}
	sequence := 0
	service, err := platformtenant.NewService(New(pool), func() time.Time { return now.Add(time.Duration(sequence) * time.Second) }, func(time.Time) (string, error) {
		sequence++
		return fmt.Sprintf("018bcfe5-6800-7000-8000-%012d", 910+sequence), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	actor := platformadmin.Actor{AdministratorID: platformadmin.AdministratorID(administratorID), SessionID: "018bcfe5-6800-7000-8000-000000000903"}
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
	if err := service.UpdateProfile(ctx, actor, tenantID, "Stale", now.Add(-time.Second), "stale edit", "request-stale"); !strings.Contains(err.Error(), platformtenant.ErrConflict.Error()) {
		t.Fatalf("stale profile update error = %v", err)
	}
	if err := service.Suspend(ctx, actor, tenantID, "security review", "request-1"); err != nil {
		t.Fatal(err)
	}
	assertStatusAndAuditCount(t, pool, tenantID, "suspended", 2)
	if err := service.Suspend(ctx, actor, tenantID, "duplicate", "request-2"); !strings.Contains(err.Error(), platformtenant.ErrInvalidTransition.Error()) {
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
	lock, err := pool.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lock.Exec(context.Background(), "SELECT pg_advisory_lock(1297040469)"); err != nil {
		lock.Release()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = lock.Exec(context.Background(), "SELECT pg_advisory_unlock(1297040469)")
		lock.Release()
	})
	if _, err := pool.Exec(context.Background(), "DROP SCHEMA IF EXISTS modura CASCADE"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DROP SCHEMA IF EXISTS modura CASCADE") })
	for _, name := range []string{"000001_initialize.up.sql", "000002_identity_foundation.up.sql", "000003_organization_foundation.up.sql", "000004_authorization_and_provisioning.up.sql", "000005_platform_identity.up.sql", "000006_platform_tenant_audit.up.sql", "000007_authorization_policies.up.sql", "000008_audit_state_snapshots.up.sql", "000009_settings_foundation.up.sql"} {
		migration, err := os.ReadFile(filepath.Join("..", "..", "..", "platform", "database", "migrations", name))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(context.Background(), string(migration)); err != nil {
			t.Fatalf("apply %s: %v", name, err)
		}
	}
	return pool
}
