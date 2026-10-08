package identity

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

type memoryStore struct {
	account        Account
	session        NewSession
	current        [32]byte
	previous       [32]byte
	revoked        bool
	oneTimeHash    [32]byte
	oneTimePurpose OneTimePurpose
	oneTimeExpiry  time.Time
	oneTimeUsed    bool
	users          map[UserID]TenantUser
	guardFailures  map[string]int
	guardLocked    map[string]time.Time
	securityEvents []SecurityEventType
}

func (m *memoryStore) guardKey(slug, login string) string { return slug + "\x00" + login }

func (m *memoryStore) FindActiveAccount(_ context.Context, tenant, login string) (Account, error) {
	if tenant != "acme" || login != "alice" {
		return Account{}, ErrInvalidCredentials
	}
	return m.account, nil
}
func (m *memoryStore) UpdatePasswordHash(_ context.Context, tenant TenantID, user UserID, hash string) error {
	if tenant != m.account.TenantID || user != m.account.UserID {
		return ErrInvalidCredentials
	}
	m.account.PasswordHash = hash
	return nil
}
func (m *memoryStore) CreateSession(_ context.Context, session NewSession) error {
	m.session, m.current = session, session.RefreshHash
	return nil
}
func (m *memoryStore) RotateSession(_ context.Context, presented, next [32]byte, now, expires time.Time, _ string) (Session, error) {
	if m.revoked {
		return Session{}, ErrInvalidToken
	}
	if presented == m.previous {
		m.revoked = true
		m.securityEvents = append(m.securityEvents, SecurityEventRefreshReplayDetected)
		return Session{}, ErrRefreshReuse
	}
	if presented != m.current {
		return Session{}, ErrInvalidToken
	}
	m.previous, m.current = m.current, next
	m.LastUsedAtForTest(now, expires)
	return m.session.Session, nil
}
func (m *memoryStore) RevokeSession(_ context.Context, tenant TenantID, user UserID, session SessionID, _ string, _ time.Time, _ string) error {
	m.securityEvents = append(m.securityEvents, SecurityEventSessionsRevoked)
	if tenant != m.session.TenantID || user != m.session.UserID || session != m.session.ID {
		return ErrInvalidToken
	}
	m.revoked = true
	return nil
}
func (m *memoryStore) RevokeOtherSessions(context.Context, TenantID, UserID, SessionID, string, time.Time) error {
	return nil
}
func (m *memoryStore) RevokeAllSessions(_ context.Context, tenant TenantID, user UserID, _ string, _ time.Time, _ string) error {
	if tenant != m.session.TenantID || user != m.session.UserID {
		return ErrInvalidToken
	}
	m.revoked = true
	m.securityEvents = append(m.securityEvents, SecurityEventSessionsRevoked)
	return nil
}

func (m *memoryStore) LoginGuardLockedUntil(_ context.Context, slug, login string, now time.Time) (time.Time, error) {
	until, ok := m.guardLocked[m.guardKey(slug, login)]
	if !ok || !until.After(now) {
		return time.Time{}, nil
	}
	return until, nil
}

func (m *memoryStore) RecordLoginFailure(_ context.Context, slug, login string, _, _ time.Time, lockUntil time.Time, threshold int32) (bool, bool, error) {
	key := m.guardKey(slug, login)
	if m.guardFailures == nil {
		m.guardFailures = map[string]int{}
	}
	m.guardFailures[key]++
	count := m.guardFailures[key]
	first := count == 1
	locked := int32(count) >= threshold
	if locked {
		if m.guardLocked == nil {
			m.guardLocked = map[string]time.Time{}
		}
		m.guardLocked[key] = lockUntil
	}
	return first, locked, nil
}

func (m *memoryStore) ClearLoginGuard(_ context.Context, slug, login string) error {
	key := m.guardKey(slug, login)
	delete(m.guardFailures, key)
	delete(m.guardLocked, key)
	return nil
}

func (m *memoryStore) RecordSecurityEvent(_ context.Context, eventType SecurityEventType, _ TenantID, _ UserID, _ string, _ time.Time) error {
	m.securityEvents = append(m.securityEvents, eventType)
	return nil
}
func (m *memoryStore) ValidateSession(_ context.Context, session Session, now time.Time) error {
	if m.revoked || session.ID != m.session.ID || session.TenantID != m.session.TenantID || session.UserID != m.session.UserID || session.SecurityVersion != m.session.SecurityVersion || !m.session.ExpiresAt.After(now) {
		return ErrInvalidToken
	}
	return nil
}
func (m *memoryStore) PasswordHash(_ context.Context, actor Actor) (string, error) {
	if actor.TenantID != m.account.TenantID || actor.UserID != m.account.UserID || actor.SessionID != m.session.ID {
		return "", ErrInvalidCredentials
	}
	return m.account.PasswordHash, nil
}
func (m *memoryStore) Profile(context.Context, Actor) (Profile, error) {
	return Profile{ID: m.account.UserID, Username: "alice", Status: "active"}, nil
}
func (m *memoryStore) ListUsers(context.Context, TenantID) ([]TenantUser, error) { return nil, nil }
func (m *memoryStore) GetUser(context.Context, TenantID, UserID) (TenantUser, error) {
	return TenantUser{}, ErrUserNotFound
}
func (m *memoryStore) GetUserTx(_ context.Context, _ pgx.Tx, tenant TenantID, user UserID) (TenantUser, error) {
	if tenant != m.account.TenantID {
		return TenantUser{}, ErrUserNotFound
	}
	record, ok := m.users[user]
	if !ok {
		return TenantUser{}, ErrUserNotFound
	}
	return record, nil
}
func (m *memoryStore) UserExistsTx(_ context.Context, _ pgx.Tx, tenant TenantID, user UserID) (bool, error) {
	if tenant != m.account.TenantID {
		return false, nil
	}
	_, ok := m.users[user]
	return ok, nil
}
func (*memoryStore) ListTenantSummaries(context.Context) ([]TenantSummary, error) {
	return nil, nil
}
func (*memoryStore) LockTenantProfile(context.Context, pgx.Tx, TenantID) (TenantProfileState, error) {
	return TenantProfileState{}, ErrTenantNotFound
}
func (*memoryStore) UpdateTenantProfile(context.Context, pgx.Tx, TenantID, string, time.Time) error {
	return nil
}
func (*memoryStore) TransitionTenantStatus(context.Context, pgx.Tx, TenantID, string, string, time.Time) error {
	return nil
}
func (m *memoryStore) UpdateProfile(_ context.Context, _ pgx.Tx, change ProfileChange) (Profile, Profile, error) {
	if change.Actor.TenantID != m.account.TenantID || change.Actor.UserID != m.account.UserID {
		return Profile{}, Profile{}, ErrInvalidToken
	}
	before := Profile{ID: change.Actor.UserID, Username: "alice", Status: "active", UpdatedAt: change.OccurredAt.Add(-time.Minute)}
	after := Profile{ID: change.Actor.UserID, Username: change.Username, Email: change.Email, Status: "active", UpdatedAt: change.OccurredAt}
	return before, after, nil
}
func (m *memoryStore) ChangePassword(_ context.Context, actor Actor, expectedHash, newHash string, presented, next [32]byte, now, expires time.Time, _ string) (Session, error) {
	m.securityEvents = append(m.securityEvents, SecurityEventPasswordChanged)
	if m.revoked || actor.SessionID != m.session.ID || expectedHash != m.account.PasswordHash || presented != m.current {
		return Session{}, ErrInvalidToken
	}
	m.previous, m.current = m.current, next
	m.account.PasswordHash = newHash
	m.account.SecurityVersion++
	m.session.SecurityVersion = m.account.SecurityVersion
	m.LastUsedAtForTest(now, expires)
	return m.session.Session, nil
}
func (m *memoryStore) CreateOneTimeToken(_ context.Context, tenant TenantID, user UserID, purpose OneTimePurpose, _ string, hash [32]byte, _ time.Time, expires time.Time) error {
	if tenant != m.account.TenantID || user != m.account.UserID {
		return ErrInactiveUser
	}
	m.oneTimeHash, m.oneTimePurpose, m.oneTimeExpiry, m.oneTimeUsed = hash, purpose, expires, false
	return nil
}
func (m *memoryStore) ConsumeOneTimeToken(_ context.Context, hash [32]byte, purpose OneTimePurpose, passwordHash string, now time.Time, _ string) error {
	m.securityEvents = append(m.securityEvents, SecurityEventPasswordChanged)
	if m.oneTimeUsed || hash != m.oneTimeHash || purpose != m.oneTimePurpose {
		return ErrInvalidToken
	}
	if !m.oneTimeExpiry.After(now) {
		return ErrExpiredToken
	}
	m.oneTimeUsed, m.revoked = true, true
	m.account.PasswordHash = passwordHash
	m.account.SecurityVersion++
	return nil
}
func (m *memoryStore) DisableAccount(_ context.Context, _ pgx.Tx, tenant TenantID, user UserID, _ string, _ string, now time.Time) error {
	if tenant != m.account.TenantID {
		return ErrUserNotFound
	}
	record, ok := m.users[user]
	if !ok {
		return ErrUserNotFound
	}
	if record.Status != "disabled" {
		record.Status, record.UpdatedAt = "disabled", now
		m.users[user] = record
	}
	m.revoked = true
	return nil
}
func (m *memoryStore) UnlockAccount(_ context.Context, _ pgx.Tx, tenant TenantID, user UserID, now time.Time) error {
	if tenant != m.account.TenantID {
		return ErrUserNotFound
	}
	record, ok := m.users[user]
	if !ok {
		return ErrUserNotFound
	}
	switch record.Status {
	case "locked":
		record.Status, record.UpdatedAt = "active", now
		m.users[user] = record
		return nil
	case "active":
		return nil
	default:
		return ErrInactiveUser
	}
}
func (*memoryStore) ProvisionTenant(context.Context, pgx.Tx, TenantProvisioning) error { return nil }
func (*memoryStore) ActivateTenant(context.Context, pgx.Tx, TenantID, time.Time) error { return nil }
func (m *memoryStore) LastUsedAtForTest(_, expires time.Time)                          { m.session.ExpiresAt = expires }

func TestLoginRefreshAndReplay(t *testing.T) {
	passwords := DefaultPasswordParameters()
	hash, err := HashPassword("correct horse battery staple", passwords)
	if err != nil {
		t.Fatal(err)
	}
	store := &memoryStore{account: Account{TenantID: "tenant-1", UserID: "user-1", PasswordHash: hash, SecurityVersion: 1}}
	now := time.Unix(1_700_000_000, 0)
	signer, err := NewAccessTokenSigner("wheretolive", "admin", "key-1", []byte(strings.Repeat("k", 32)), 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	sequence := 0
	verifier := NewAccessTokenVerifier("wheretolive", "admin", map[string][]byte{"key-1": []byte(strings.Repeat("k", 32))}, 5*time.Second)
	service, err := NewService(store, signer, verifier, passwords, 24*time.Hour, func() time.Time { return now }, func(time.Time) (string, error) {
		sequence++
		return fmt.Sprintf("id-%d", sequence), nil
	}, func() (string, error) {
		sequence++
		return fmt.Sprintf("%064d", sequence), nil
	})
	if err != nil {
		t.Fatal(err)
	}

	tokens, err := service.Login(context.Background(), " ACME ", "Alice", "correct horse battery staple", "request-login")
	if err != nil || tokens.AccessToken == "" || tokens.RefreshToken == "" {
		t.Fatalf("tokens=%+v err=%v", tokens, err)
	}
	rotated, err := service.Refresh(context.Background(), tokens.RefreshToken, "request-refresh")
	if err != nil || rotated.RefreshToken == tokens.RefreshToken {
		t.Fatalf("tokens=%+v err=%v", rotated, err)
	}
	if _, err := service.Refresh(context.Background(), tokens.RefreshToken, "request-refresh"); !errors.Is(err, ErrRefreshReuse) {
		t.Fatalf("replay err=%v", err)
	}
	if _, err := service.Refresh(context.Background(), rotated.RefreshToken, "request-refresh"); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("family err=%v", err)
	}
	if _, err := service.AuthenticateAccess(context.Background(), tokens.AccessToken); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("revoked access err=%v", err)
	}
}

func TestLoginUsesGenericCredentialFailure(t *testing.T) {
	passwords := DefaultPasswordParameters()
	hash, err := HashPassword("correct horse battery staple", passwords)
	if err != nil {
		t.Fatal(err)
	}
	store := &memoryStore{account: Account{TenantID: "tenant-1", UserID: "user-1", PasswordHash: hash, SecurityVersion: 1}}
	signer, _ := NewAccessTokenSigner("wheretolive", "admin", "key-1", []byte(strings.Repeat("k", 32)), time.Minute)
	verifier := NewAccessTokenVerifier("wheretolive", "admin", map[string][]byte{"key-1": []byte(strings.Repeat("k", 32))}, 0)
	service, _ := NewService(store, signer, verifier, passwords, time.Hour, time.Now, func(time.Time) (string, error) { return "id", nil }, func() (string, error) { return strings.Repeat("s", 32), nil })
	for _, test := range []struct{ tenant, login, password string }{{"missing", "alice", "correct horse battery staple"}, {"acme", "alice", "wrong password"}} {
		if _, err := service.Login(context.Background(), test.tenant, test.login, test.password, "request-login"); !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("err=%v", err)
		}
	}
}

func TestChangePasswordRotatesSecurityState(t *testing.T) {
	passwords := DefaultPasswordParameters()
	hash, err := HashPassword("correct horse battery staple", passwords)
	if err != nil {
		t.Fatal(err)
	}
	store := &memoryStore{account: Account{TenantID: "tenant-1", UserID: "user-1", PasswordHash: hash, SecurityVersion: 1}}
	now := time.Unix(1_700_000_000, 0)
	key := []byte(strings.Repeat("k", 32))
	signer, _ := NewAccessTokenSigner("wheretolive", "admin", "key-1", key, 5*time.Minute)
	verifier := NewAccessTokenVerifier("wheretolive", "admin", map[string][]byte{"key-1": key}, 0)
	sequence := 0
	service, err := NewService(store, signer, verifier, passwords, time.Hour, func() time.Time { return now }, func(time.Time) (string, error) {
		sequence++
		return fmt.Sprintf("id-%d", sequence), nil
	}, func() (string, error) {
		sequence++
		return fmt.Sprintf("%064d", sequence), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	loggedIn, err := service.Login(context.Background(), "acme", "alice", "correct horse battery staple", "request-login")
	if err != nil {
		t.Fatal(err)
	}
	actor := Actor{TenantID: "tenant-1", UserID: "user-1", SessionID: store.session.ID}
	changed, err := service.ChangePassword(context.Background(), actor, "correct horse battery staple", "a newly secured password", loggedIn.RefreshToken, "request-password")
	if err != nil {
		t.Fatal(err)
	}
	claims, err := verifier.Verify(changed.AccessToken, now)
	if err != nil || claims.SecurityVersion != 2 {
		t.Fatalf("claims=%+v err=%v", claims, err)
	}
	valid, _, err := VerifyPassword("a newly secured password", store.account.PasswordHash, passwords)
	if err != nil || !valid {
		t.Fatalf("new password valid=%v err=%v", valid, err)
	}
}

func TestOneTimeTokenIsSingleUseAndRevokesSessions(t *testing.T) {
	passwords := DefaultPasswordParameters()
	oldHash, err := HashPassword("correct horse battery staple", passwords)
	if err != nil {
		t.Fatal(err)
	}
	store := &memoryStore{account: Account{TenantID: "tenant-1", UserID: "user-1", PasswordHash: oldHash, SecurityVersion: 1}}
	now := time.Unix(1_700_000_000, 0)
	key := []byte(strings.Repeat("k", 32))
	signer, _ := NewAccessTokenSigner("wheretolive", "admin", "key-1", key, time.Minute)
	verifier := NewAccessTokenVerifier("wheretolive", "admin", map[string][]byte{"key-1": key}, 0)
	service, err := NewService(store, signer, verifier, passwords, time.Hour, func() time.Time { return now }, func(time.Time) (string, error) { return "token-id", nil }, func() (string, error) { return strings.Repeat("r", 32), nil })
	if err != nil {
		t.Fatal(err)
	}
	secret, err := service.IssueOneTimeToken(context.Background(), "tenant-1", "user-1", PurposePasswordReset, 15*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.ConsumeOneTimeToken(context.Background(), secret, PurposePasswordReset, "a newly recovered password", "request-consume"); err != nil {
		t.Fatal(err)
	}
	if err := service.ConsumeOneTimeToken(context.Background(), secret, PurposePasswordReset, "another secure password", "request-consume"); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("second consumption error = %v", err)
	}
	valid, _, err := VerifyPassword("a newly recovered password", store.account.PasswordHash, passwords)
	if err != nil || !valid || !store.revoked || store.account.SecurityVersion != 2 {
		t.Fatalf("valid=%v revoked=%v version=%d err=%v", valid, store.revoked, store.account.SecurityVersion, err)
	}
	expiring, err := service.IssueOneTimeToken(context.Background(), "tenant-1", "user-1", PurposePasswordReset, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Minute)
	if err := service.ConsumeOneTimeToken(context.Background(), expiring, PurposePasswordReset, "another secure password", "request-consume"); !errors.Is(err, ErrExpiredToken) {
		t.Fatalf("expired token error = %v", err)
	}
}

func TestDisableUserIsTenantScopedAndRecordsTransactionalAudit(t *testing.T) {
	service, store, auditor := newAdministrativeTestService(t)
	actor := Actor{TenantID: "tenant-1", UserID: "manager-9", SessionID: "session-9"}
	if _, err := service.DisableUser(context.Background(), Actor{TenantID: "other-tenant", UserID: "manager-9", SessionID: "session-9"}, "user-1", "administrative disable", "request-disable"); err == nil {
		t.Fatal("cross-tenant disable succeeded")
	}
	if _, err := service.DisableUser(context.Background(), actor, "manager-9", "administrative disable", "request-disable"); err == nil {
		t.Fatal("self disable succeeded")
	}
	if _, err := service.DisableUser(context.Background(), actor, "user-1", "   ", "request-disable"); err == nil {
		t.Fatal("blank reason disable succeeded")
	}
	if _, err := service.DisableUser(context.Background(), actor, "user-1", "administrative disable", " "); err == nil {
		t.Fatal("blank correlation disable succeeded")
	}
	disabled, err := service.DisableUser(context.Background(), actor, "user-1", "administrative disable", "request-disable")
	if err != nil || disabled.Status != "disabled" {
		t.Fatalf("disable err=%v user=%+v", err, disabled)
	}
	if !store.revoked {
		t.Fatal("disable did not revoke sessions")
	}
	if len(auditor.events) != 1 {
		t.Fatalf("audit events = %d", len(auditor.events))
	}
	event := auditor.events[0]
	if event.Action != "identity.user.disabled" || event.Resource != "user" || event.ResourceID != "user-1" || event.Reason != "administrative disable" || event.CorrelationID != "request-disable" || event.TargetUserID != "user-1" {
		t.Fatalf("audit event = %+v", event)
	}
	if !strings.Contains(string(event.BeforeState), `"active"`) || !strings.Contains(string(event.AfterState), `"disabled"`) {
		t.Fatalf("audit states before=%s after=%s", event.BeforeState, event.AfterState)
	}
	if _, ok := auditor.tx.(testTx); !ok {
		t.Fatal("audit was not recorded inside the caller transaction")
	}
	// Repeated disable is idempotent and still audited.
	if _, err := service.DisableUser(context.Background(), actor, "user-1", "administrative disable", "request-disable-2"); err != nil {
		t.Fatal(err)
	}
	if len(auditor.events) != 2 {
		t.Fatalf("audit events after replay = %d", len(auditor.events))
	}
}

func TestDisableUserRollsBackWhenAuditFails(t *testing.T) {
	service, store, auditor := newAdministrativeTestService(t)
	auditor.fail = true
	actor := Actor{TenantID: "tenant-1", UserID: "manager-9", SessionID: "session-9"}
	if _, err := service.DisableUser(context.Background(), actor, "user-1", "administrative disable", "request-disable"); err == nil {
		t.Fatal("disable succeeded although audit failed")
	}
	if store.users["user-1"].Status != "active" || store.revoked {
		t.Fatalf("disable was not rolled back: %+v revoked=%v", store.users["user-1"], store.revoked)
	}
}

func TestUnlockUserAppliesOnlyToLockedAccounts(t *testing.T) {
	service, store, auditor := newAdministrativeTestService(t)
	actor := Actor{TenantID: "tenant-1", UserID: "manager-9", SessionID: "session-9"}
	if _, err := service.UnlockUser(context.Background(), actor, "missing-user", "request-unlock"); !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("missing user error = %v", err)
	}
	store.users["disabled-user"] = TenantUser{ID: "disabled-user", Username: "dana", Status: "disabled"}
	if _, err := service.UnlockUser(context.Background(), actor, "disabled-user", "request-unlock"); !errors.Is(err, ErrInactiveUser) {
		t.Fatalf("disabled user error = %v", err)
	}
	if _, err := service.DisableUser(context.Background(), Actor{TenantID: "tenant-1", UserID: "manager-9", SessionID: "session-9"}, "manager-9", "self", "request-unlock"); err == nil {
		t.Fatal("self unlock guard failed")
	}
	store.users["active-user"] = TenantUser{ID: "active-user", Username: "adam", Status: "active"}
	if _, err := service.UnlockUser(context.Background(), actor, "active-user", "request-unlock"); err != nil {
		t.Fatalf("idempotent active unlock error = %v", err)
	}
	store.users["locked-user"] = TenantUser{ID: "locked-user", Username: "lars", Status: "locked"}
	unlocked, err := service.UnlockUser(context.Background(), actor, "locked-user", "request-unlock")
	if err != nil || unlocked.Status != "active" {
		t.Fatalf("unlock err=%v user=%+v", err, unlocked)
	}
	// The idempotent active unlock and the locked unlock are both audited.
	if len(auditor.events) != 2 {
		t.Fatalf("unlock audit events = %d", len(auditor.events))
	}
	event := auditor.events[1]
	if event.Action != "identity.user.unlocked" || event.ResourceID != "locked-user" || event.Reason == "" {
		t.Fatalf("unlock audit event = %+v", event)
	}
	if !strings.Contains(string(event.BeforeState), `"locked"`) || !strings.Contains(string(event.AfterState), `"active"`) {
		t.Fatalf("unlock audit states before=%s after=%s", event.BeforeState, event.AfterState)
	}
}

type testTx struct{ pgx.Tx }

// testTransactor applies work on the memory store and restores its snapshot
// when the work fails, mirroring database commit and rollback semantics.
type testTransactor struct{ store *memoryStore }

func (t testTransactor) WithinTransaction(_ context.Context, work func(pgx.Tx) error) error {
	snapshotUsers := make(map[UserID]TenantUser, len(t.store.users))
	for id, user := range t.store.users {
		snapshotUsers[id] = user
	}
	snapshotRevoked := t.store.revoked
	if err := work(testTx{}); err != nil {
		t.store.users = snapshotUsers
		t.store.revoked = snapshotRevoked
		return err
	}
	return nil
}

type testAuditor struct {
	events []AccountAuditEvent
	tx     pgx.Tx
	fail   bool
}

func (a *testAuditor) RecordAccountEvent(_ context.Context, tx pgx.Tx, event AccountAuditEvent) error {
	if a.fail {
		return fmt.Errorf("audit write failed")
	}
	a.tx = tx
	a.events = append(a.events, event)
	return nil
}

func newAdministrativeTestService(t *testing.T) (*Service, *memoryStore, *testAuditor) {
	t.Helper()
	store := &memoryStore{
		account: Account{TenantID: "tenant-1", UserID: "user-1", SecurityVersion: 1},
		users: map[UserID]TenantUser{
			"user-1": {ID: "user-1", Username: "alice", Status: "active"},
		},
	}
	key := []byte(strings.Repeat("k", 32))
	signer, _ := NewAccessTokenSigner("wheretolive", "admin", "key-1", key, time.Minute)
	verifier := NewAccessTokenVerifier("wheretolive", "admin", map[string][]byte{"key-1": key}, 0)
	service, err := NewService(store, signer, verifier, DefaultPasswordParameters(), time.Hour, time.Now, func(time.Time) (string, error) { return "id", nil }, func() (string, error) { return strings.Repeat("s", 32), nil })
	if err != nil {
		t.Fatal(err)
	}
	auditor := &testAuditor{}
	if err := service.EnableUserManagement(testTransactor{store: store}, auditor); err != nil {
		t.Fatal(err)
	}
	return service, store, auditor
}

func TestLoginThrottlingLocksAndRecordsSecurityEvents(t *testing.T) {
	passwords := DefaultPasswordParameters()
	hash, err := HashPassword("correct horse battery staple", passwords)
	if err != nil {
		t.Fatal(err)
	}
	store := &memoryStore{account: Account{TenantID: "tenant-1", UserID: "user-1", PasswordHash: hash, SecurityVersion: 1}}
	key := []byte(strings.Repeat("k", 32))
	signer, _ := NewAccessTokenSigner("wheretolive", "admin", "key-1", key, time.Minute)
	verifier := NewAccessTokenVerifier("wheretolive", "admin", map[string][]byte{"key-1": key}, 0)
	service, err := NewService(store, signer, verifier, passwords, time.Hour, time.Now, func(time.Time) (string, error) { return "id", nil }, func() (string, error) { return strings.Repeat("s", 32), nil })
	if err != nil {
		t.Fatal(err)
	}
	// Five wrong attempts are rejected as invalid credentials; the fifth
	// triggers the lockout.
	for i := 1; i <= 5; i++ {
		if _, err := service.Login(context.Background(), "acme", "alice", "wrong password", "request-login"); !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("attempt %d error = %v", i, err)
		}
	}
	// The lockout applies even to the correct credential pair.
	if _, err := service.Login(context.Background(), "acme", "alice", "correct horse battery staple", "request-login"); !errors.Is(err, ErrAccountLocked) {
		t.Fatalf("locked login error = %v", err)
	}
	events := map[SecurityEventType]int{}
	for _, event := range store.securityEvents {
		events[event]++
	}
	if events[SecurityEventLoginFailed] != 1 || events[SecurityEventLoginLocked] != 1 {
		t.Fatalf("security events = %v", events)
	}
}
