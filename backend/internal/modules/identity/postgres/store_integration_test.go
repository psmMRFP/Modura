package postgres

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
	"github.com/modura-dev/modura/backend/internal/platform/database"
	"github.com/modura-dev/modura/backend/internal/platform/database/migrationtest"
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
	alphaUsers, err := store.ListUsers(ctx, alpha.TenantID)
	if err != nil {
		t.Fatal(err)
	}
	if len(alphaUsers) != 2 {
		t.Fatalf("alpha user count = %d", len(alphaUsers))
	}
	if _, err := store.GetUser(ctx, alpha.TenantID, beta.UserID); !errors.Is(err, identity.ErrUserNotFound) {
		t.Fatalf("cross-tenant user lookup = %v", err)
	}
	invitation := identity.HashOpaqueToken("invitation-secret-that-is-long-enough")
	if err := store.CreateOneTimeToken(ctx, alpha.TenantID, "018bcfe5-6800-7000-8000-000000000013", identity.PurposeInvitation, "018bcfe5-6800-7000-8000-000000000031", invitation, now, now.Add(15*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := store.ConsumeOneTimeToken(ctx, invitation, identity.PurposeInvitation, "new-argon-hash", now.Add(time.Minute), "request-invitation"); err != nil {
		t.Fatal(err)
	}
	if err := store.ConsumeOneTimeToken(ctx, invitation, identity.PurposeInvitation, "replacement", now.Add(2*time.Minute), "request-invitation"); !errors.Is(err, identity.ErrInvalidToken) {
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
	profileTx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatal(err)
	}
	before, profile, err := store.UpdateProfile(ctx, profileTx, identity.ProfileChange{Actor: identity.Actor{TenantID: alpha.TenantID, UserID: alpha.UserID, SessionID: session.ID}, Username: "Shared Admin", NormalizedUsername: "shared admin", Email: &email, NormalizedEmail: &normalizedEmail, CorrelationID: "request-profile", OccurredAt: now.Add(30 * time.Second)})
	if err != nil {
		t.Fatal(err)
	}
	if err := profileTx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if profile.Username != "Shared Admin" || profile.Email == nil || *profile.Email != email {
		t.Fatalf("profile = %+v", profile)
	}
	if before.Username == profile.Username {
		t.Fatalf("before snapshot = %+v", before)
	}
	if _, err := store.RotateSession(ctx, first, second, now.Add(time.Minute), now.Add(time.Hour), "request-refresh"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RotateSession(ctx, first, identity.HashOpaqueToken("third-refresh-secret-that-is-long-enough"), now.Add(2*time.Minute), now.Add(time.Hour), "request-refresh"); !errors.Is(err, identity.ErrRefreshReuse) {
		t.Fatalf("replay error = %v", err)
	}
	if err := store.ValidateSession(ctx, session.Session, now.Add(3*time.Minute)); !errors.Is(err, identity.ErrInvalidToken) {
		t.Fatalf("family remains active after replay: %v", err)
	}
	betaSession := identity.NewSession{Session: identity.Session{ID: "018bcfe5-6800-7000-8000-000000000023", TenantID: beta.TenantID, UserID: beta.UserID, SecurityVersion: 1}, FamilyID: "018bcfe5-6800-7000-8000-000000000024", RefreshHash: identity.HashOpaqueToken("beta-refresh-secret-that-is-long-enough"), CreatedAt: now, ExpiresAt: now.Add(time.Hour)}
	if err := store.CreateSession(ctx, betaSession); err != nil {
		t.Fatal(err)
	}
	disableTx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.DisableAccount(ctx, disableTx, beta.TenantID, beta.UserID, "administrative_disable", "request-disable", now.Add(4*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := disableTx.Commit(ctx); err != nil {
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
	unlockTx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = unlockTx.Rollback(ctx) }()
	if err := store.UnlockAccount(ctx, unlockTx, beta.TenantID, beta.UserID, now.Add(6*time.Minute)); !errors.Is(err, identity.ErrInactiveUser) {
		t.Fatalf("disabled account unlock error = %v", err)
	}
}

// TestLoginThrottlingAndSecurityEvents exercises the credential guard and
// privacy-conscious security events against real PostgreSQL.
func TestLoginThrottlingAndSecurityEvents(t *testing.T) {
	pool := integrationPool(t)
	ctx := context.Background()
	now := time.Unix(1_700_000_000, 0).UTC()
	if _, err := pool.Exec(ctx, `INSERT INTO modura.tenants (id, slug, display_name, status, created_at, updated_at) VALUES ('018bcfe5-6800-7000-8000-000000000301', 'gamma', 'Gamma', 'active', $1, $1)`, now); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO modura.users (id, tenant_id, username, normalized_username, password_hash, status, created_at, updated_at) VALUES ('018bcfe5-6800-7000-8000-000000000311', '018bcfe5-6800-7000-8000-000000000301', 'manager', 'manager', 'hash-manager', 'active', $1, $1)`, now); err != nil {
		t.Fatal(err)
	}
	store := New(pool)
	// Guard state is keyed by the submitted slug and login only.
	for i := 1; i <= 5; i++ {
		first, locked, err := store.RecordLoginFailure(ctx, "gamma", "missing-user", now.Add(time.Duration(i)*time.Second), now, now.Add(time.Hour), 5)
		if err != nil {
			t.Fatal(err)
		}
		if first != (i == 1) {
			t.Fatalf("attempt %d first=%v", i, first)
		}
		if locked != (i == 5) {
			t.Fatalf("attempt %d locked=%v", i, locked)
		}
	}
	lockedUntil, err := store.LoginGuardLockedUntil(ctx, "gamma", "missing-user", now.Add(10*time.Second))
	if err != nil || lockedUntil.IsZero() {
		t.Fatalf("guard locked until=%v err=%v", lockedUntil, err)
	}
	if until, err := store.LoginGuardLockedUntil(ctx, "gamma", "missing-user", now.Add(2*time.Hour)); err != nil || !until.IsZero() {
		t.Fatalf("expired guard until=%v err=%v", until, err)
	}
	if err := store.RecordSecurityEvent(ctx, identity.SecurityEventSessionsRevoked, "018bcfe5-6800-7000-8000-000000000301", "018bcfe5-6800-7000-8000-000000000311", "request-security", now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	var eventCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM modura.auth_security_events WHERE event_type = 'sessions_revoked' AND tenant_id = $1 AND user_id = $2`, "018bcfe5-6800-7000-8000-000000000301", "018bcfe5-6800-7000-8000-000000000311").Scan(&eventCount); err != nil {
		t.Fatal(err)
	}
	if eventCount != 1 {
		t.Fatalf("security event count = %d", eventCount)
	}
	if err := store.ClearLoginGuard(ctx, "gamma", "missing-user"); err != nil {
		t.Fatal(err)
	}
	if until, err := store.LoginGuardLockedUntil(ctx, "gamma", "missing-user", now.Add(10*time.Second)); err != nil || !until.IsZero() {
		t.Fatalf("cleared guard until=%v err=%v", until, err)
	}
}

// TestDisableUserWorkflowRecordsTransactionalAudit exercises the full
// management workflow with the real audit store so the audit row and the
// account write demonstrably commit or roll back together.
func TestDisableUserWorkflowRecordsTransactionalAudit(t *testing.T) {
	pool := integrationPool(t)
	ctx := context.Background()
	now := time.Unix(1_700_000_000, 0).UTC()
	if _, err := pool.Exec(ctx, `INSERT INTO modura.tenants (id, slug, display_name, status, created_at, updated_at) VALUES ('018bcfe5-6800-7000-8000-000000000301', 'gamma', 'Gamma', 'active', $1, $1)`, now); err != nil {
		t.Fatal(err)
	}
	seedUsers := `
INSERT INTO modura.users (id, tenant_id, username, normalized_username, password_hash, status, created_at, updated_at) VALUES
('018bcfe5-6800-7000-8000-000000000311', '018bcfe5-6800-7000-8000-000000000301', 'manager', 'manager', 'hash-manager', 'active', $1, $1),
('018bcfe5-6800-7000-8000-000000000312', '018bcfe5-6800-7000-8000-000000000301', 'target', 'target', 'hash-target', 'active', $1, $1)`
	if _, err := pool.Exec(ctx, seedUsers, now); err != nil {
		t.Fatal(err)
	}
	sequence := 0
	newID := func(time.Time) (string, error) {
		sequence++
		return fmt.Sprintf("018bcfe5-6800-7000-8000-%012d", sequence), nil
	}
	auditService, err := audit.NewService(auditpostgres.New(pool), newID)
	if err != nil {
		t.Fatal(err)
	}
	key := []byte(strings.Repeat("k", 32))
	signer, err := identity.NewAccessTokenSigner("modura", "admin", "key-1", key, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	verifier := identity.NewAccessTokenVerifier("modura", "admin", map[string][]byte{"key-1": key}, 0)
	store := New(pool)
	identityService, err := identity.NewService(store, signer, verifier, identity.DefaultPasswordParameters(), time.Hour, time.Now, newID, func() (string, error) { return strings.Repeat("s", 32), nil })
	if err != nil {
		t.Fatal(err)
	}
	if err := identityService.EnableUserManagement(database.NewTransactor(pool), workflowAuditor{service: auditService}); err != nil {
		t.Fatal(err)
	}
	actor := identity.Actor{TenantID: "018bcfe5-6800-7000-8000-000000000301", UserID: "018bcfe5-6800-7000-8000-000000000311", SessionID: "018bcfe5-6800-7000-8000-000000000321"}
	disabled, err := identityService.DisableUser(ctx, actor, "018bcfe5-6800-7000-8000-000000000312", "policy violation", "request-disable-workflow")
	if err != nil || disabled.Status != "disabled" {
		t.Fatalf("disable err=%v user=%+v", err, disabled)
	}
	var action, resource, resourceID, result string
	if err := pool.QueryRow(ctx, `SELECT action, resource, resource_id, result FROM modura.audit_events WHERE tenant_id = $1 AND correlation_id = 'request-disable-workflow'`, actor.TenantID).Scan(&action, &resource, &resourceID, &result); err != nil {
		t.Fatalf("transactional audit row missing: %v", err)
	}
	if action != "identity.user.disabled" || resource != "user" || resourceID != "018bcfe5-6800-7000-8000-000000000312" || result != "succeeded" {
		t.Fatalf("audit row action=%q resource=%q resource_id=%q result=%q", action, resource, resourceID, result)
	}
	lockedAt := now.Add(time.Minute)
	if _, err := pool.Exec(ctx, `UPDATE modura.users SET status = 'locked', updated_at = $3 WHERE tenant_id = $1 AND id = $2`, actor.TenantID, "018bcfe5-6800-7000-8000-000000000312", lockedAt); err != nil {
		t.Fatal(err)
	}
	unlocked, err := identityService.UnlockUser(ctx, actor, "018bcfe5-6800-7000-8000-000000000312", "request-unlock-workflow")
	if err != nil || unlocked.Status != "active" {
		t.Fatalf("unlock err=%v user=%+v", err, unlocked)
	}
	var unlockAudit int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM modura.audit_events WHERE tenant_id = $1 AND action = 'identity.user.unlocked' AND resource_id = $2`, actor.TenantID, "018bcfe5-6800-7000-8000-000000000312").Scan(&unlockAudit); err != nil {
		t.Fatal(err)
	}
	if unlockAudit != 1 {
		t.Fatalf("unlock audit count = %d", unlockAudit)
	}
	if _, err := identityService.DisableUser(ctx, actor, "018bcfe5-6800-7000-8000-000000000399", "missing user", "request-disable-missing"); !errors.Is(err, identity.ErrUserNotFound) {
		t.Fatalf("missing user error = %v", err)
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
