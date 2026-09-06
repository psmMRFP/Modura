package postgres

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/modura-dev/modura/backend/internal/modules/identity"
)

func TestTenantIsolationAndRefreshReplay(t *testing.T) {
	pool := integrationPool(t)
	ctx := context.Background()
	now := time.Unix(1_700_000_000, 0).UTC()
	seedTenants := `
INSERT INTO modura.tenants (id, slug, display_name, status, created_at, updated_at) VALUES
('018bcfe5-6800-7000-8000-000000000001', 'alpha', 'Alpha', 'active', $1, $1),
	('018bcfe5-6800-7000-8000-000000000002', 'beta', 'Beta', 'active', $1, $1)`
	seedUsers := `
INSERT INTO modura.users (id, tenant_id, username, normalized_username, password_hash, status, created_at, updated_at) VALUES
('018bcfe5-6800-7000-8000-000000000011', '018bcfe5-6800-7000-8000-000000000001', 'shared', 'shared', 'hash-alpha', 'active', $1, $1),
	('018bcfe5-6800-7000-8000-000000000012', '018bcfe5-6800-7000-8000-000000000002', 'shared', 'shared', 'hash-beta', 'active', $1, $1),
	('018bcfe5-6800-7000-8000-000000000013', '018bcfe5-6800-7000-8000-000000000001', 'invited', 'invited', NULL, 'invited', $1, $1)`
	if _, err := pool.Exec(ctx, seedTenants, now); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, seedUsers, now); err != nil {
		t.Fatal(err)
	}
	store := New(pool)
	alpha, err := store.FindActiveAccount(ctx, "alpha", "shared")
	if err != nil {
		t.Fatal(err)
	}
	beta, err := store.FindActiveAccount(ctx, "beta", "shared")
	if err != nil {
		t.Fatal(err)
	}
	if alpha.TenantID == beta.TenantID || alpha.PasswordHash != "hash-alpha" || beta.PasswordHash != "hash-beta" {
		t.Fatalf("tenant lookup leaked: alpha=%+v beta=%+v", alpha, beta)
	}
	invitation := identity.HashOpaqueToken("invitation-secret-that-is-long-enough")
	if err := store.CreateOneTimeToken(ctx, alpha.TenantID, "018bcfe5-6800-7000-8000-000000000013", identity.PurposeInvitation, "018bcfe5-6800-7000-8000-000000000031", invitation, now, now.Add(15*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := store.ConsumeOneTimeToken(ctx, invitation, identity.PurposeInvitation, "new-argon-hash", now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := store.ConsumeOneTimeToken(ctx, invitation, identity.PurposeInvitation, "replacement", now.Add(2*time.Minute)); !errors.Is(err, identity.ErrInvalidToken) {
		t.Fatalf("invitation replay error = %v", err)
	}
	var invitedStatus string
	var invitedHash string
	if err := pool.QueryRow(ctx, `SELECT status, password_hash FROM modura.users WHERE tenant_id = $1 AND id = $2`, alpha.TenantID, "018bcfe5-6800-7000-8000-000000000013").Scan(&invitedStatus, &invitedHash); err != nil {
		t.Fatal(err)
	}
	if invitedStatus != "active" || invitedHash != "new-argon-hash" {
		t.Fatalf("invited status=%q hash=%q", invitedStatus, invitedHash)
	}

	first := identity.HashOpaqueToken("first-refresh-secret-that-is-long-enough")
	second := identity.HashOpaqueToken("second-refresh-secret-that-is-long-enough")
	session := identity.NewSession{Session: identity.Session{ID: "018bcfe5-6800-7000-8000-000000000021", TenantID: alpha.TenantID, UserID: alpha.UserID, SecurityVersion: 1}, FamilyID: "018bcfe5-6800-7000-8000-000000000022", RefreshHash: first, CreatedAt: now, ExpiresAt: now.Add(time.Hour)}
	if err := store.CreateSession(ctx, session); err != nil {
		t.Fatal(err)
	}
	email := "shared@example.com"
	normalizedEmail := identity.NormalizeLogin(email)
	profile, err := store.UpdateProfile(ctx, identity.ProfileChange{Actor: identity.Actor{TenantID: alpha.TenantID, UserID: alpha.UserID, SessionID: session.ID}, Username: "Shared Admin", NormalizedUsername: "shared admin", Email: &email, NormalizedEmail: &normalizedEmail, CorrelationID: "request-profile", AuditID: "018bcfe5-6800-7000-8000-000000000032", OccurredAt: now.Add(30 * time.Second)})
	if err != nil {
		t.Fatal(err)
	}
	if profile.Username != "Shared Admin" || profile.Email == nil || *profile.Email != email {
		t.Fatalf("profile = %+v", profile)
	}
	var profileAudit int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM modura.audit_events WHERE tenant_id = $1 AND actor_id = $2 AND action = 'identity.profile-updated' AND before_state IS NOT NULL AND after_state IS NOT NULL`, alpha.TenantID, alpha.UserID).Scan(&profileAudit); err != nil {
		t.Fatal(err)
	}
	if profileAudit != 1 {
		t.Fatalf("profile audit count = %d", profileAudit)
	}
	if _, err := store.RotateSession(ctx, first, second, now.Add(time.Minute), now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RotateSession(ctx, first, identity.HashOpaqueToken("third-refresh-secret-that-is-long-enough"), now.Add(2*time.Minute), now.Add(time.Hour)); !errors.Is(err, identity.ErrRefreshReuse) {
		t.Fatalf("replay error = %v", err)
	}
	if err := store.ValidateSession(ctx, session.Session, now.Add(3*time.Minute)); !errors.Is(err, identity.ErrInvalidToken) {
		t.Fatalf("family remains active after replay: %v", err)
	}
	betaSession := identity.NewSession{Session: identity.Session{ID: "018bcfe5-6800-7000-8000-000000000023", TenantID: beta.TenantID, UserID: beta.UserID, SecurityVersion: 1}, FamilyID: "018bcfe5-6800-7000-8000-000000000024", RefreshHash: identity.HashOpaqueToken("beta-refresh-secret-that-is-long-enough"), CreatedAt: now, ExpiresAt: now.Add(time.Hour)}
	if err := store.CreateSession(ctx, betaSession); err != nil {
		t.Fatal(err)
	}
	if err := store.DisableAccount(ctx, beta.TenantID, beta.UserID, "administrative_disable", now.Add(4*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := store.ValidateSession(ctx, betaSession.Session, now.Add(5*time.Minute)); !errors.Is(err, identity.ErrInvalidToken) {
		t.Fatalf("disabled account session remains active: %v", err)
	}
	var betaStatus string
	var betaVersion int64
	if err := pool.QueryRow(ctx, `SELECT status, security_version FROM modura.users WHERE tenant_id = $1 AND id = $2`, beta.TenantID, beta.UserID).Scan(&betaStatus, &betaVersion); err != nil {
		t.Fatal(err)
	}
	if betaStatus != "disabled" || betaVersion != 2 {
		t.Fatalf("disabled status=%q version=%d", betaStatus, betaVersion)
	}
	if err := store.UnlockAccount(ctx, beta.TenantID, beta.UserID, now.Add(6*time.Minute)); !errors.Is(err, identity.ErrInactiveUser) {
		t.Fatalf("disabled account unlock error = %v", err)
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
	lockConnection, err := pool.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lockConnection.Exec(context.Background(), "SELECT pg_advisory_lock(1297040469)"); err != nil {
		lockConnection.Release()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = lockConnection.Exec(context.Background(), "SELECT pg_advisory_unlock(1297040469)")
		lockConnection.Release()
	})
	if _, err := pool.Exec(context.Background(), "DROP SCHEMA IF EXISTS modura CASCADE"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DROP SCHEMA IF EXISTS modura CASCADE") })
	for _, name := range []string{"000001_initialize.up.sql", "000002_identity_foundation.up.sql", "000006_platform_tenant_audit.up.sql", "000008_audit_state_snapshots.up.sql"} {
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
