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
	status         string
}

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
func (m *memoryStore) RotateSession(_ context.Context, presented, next [32]byte, now, expires time.Time) (Session, error) {
	if m.revoked {
		return Session{}, ErrInvalidToken
	}
	if presented == m.previous {
		m.revoked = true
		return Session{}, ErrRefreshReuse
	}
	if presented != m.current {
		return Session{}, ErrInvalidToken
	}
	m.previous, m.current = m.current, next
	m.LastUsedAtForTest(now, expires)
	return m.session.Session, nil
}
func (m *memoryStore) RevokeSession(_ context.Context, tenant TenantID, user UserID, session SessionID, _ string, _ time.Time) error {
	if tenant != m.session.TenantID || user != m.session.UserID || session != m.session.ID {
		return ErrInvalidToken
	}
	m.revoked = true
	return nil
}
func (m *memoryStore) RevokeOtherSessions(context.Context, TenantID, UserID, SessionID, string, time.Time) error {
	return nil
}
func (m *memoryStore) RevokeAllSessions(_ context.Context, tenant TenantID, user UserID, _ string, _ time.Time) error {
	if tenant != m.session.TenantID || user != m.session.UserID {
		return ErrInvalidToken
	}
	m.revoked = true
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
func (m *memoryStore) UpdateProfile(_ context.Context, change ProfileChange) (Profile, error) {
	return Profile{ID: change.Actor.UserID, Username: change.Username, Email: change.Email, Status: "active", UpdatedAt: change.OccurredAt}, nil
}
func (m *memoryStore) ChangePassword(_ context.Context, actor Actor, expectedHash, newHash string, presented, next [32]byte, now, expires time.Time) (Session, error) {
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
func (m *memoryStore) ConsumeOneTimeToken(_ context.Context, hash [32]byte, purpose OneTimePurpose, passwordHash string, now time.Time) error {
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
func (m *memoryStore) DisableAccount(_ context.Context, tenant TenantID, user UserID, _ string, _ time.Time) error {
	if tenant != m.account.TenantID || user != m.account.UserID {
		return ErrInactiveUser
	}
	if m.status != "disabled" {
		m.account.SecurityVersion++
	}
	m.status, m.revoked = "disabled", true
	return nil
}
func (m *memoryStore) UnlockAccount(_ context.Context, tenant TenantID, user UserID, _ time.Time) error {
	if tenant != m.account.TenantID || user != m.account.UserID || m.status == "disabled" {
		return ErrInactiveUser
	}
	m.status = "active"
	return nil
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
	signer, err := NewAccessTokenSigner("modura", "admin", "key-1", []byte(strings.Repeat("k", 32)), 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	sequence := 0
	verifier := NewAccessTokenVerifier("modura", "admin", map[string][]byte{"key-1": []byte(strings.Repeat("k", 32))}, 5*time.Second)
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

	tokens, err := service.Login(context.Background(), " ACME ", "Alice", "correct horse battery staple")
	if err != nil || tokens.AccessToken == "" || tokens.RefreshToken == "" {
		t.Fatalf("tokens=%+v err=%v", tokens, err)
	}
	rotated, err := service.Refresh(context.Background(), tokens.RefreshToken)
	if err != nil || rotated.RefreshToken == tokens.RefreshToken {
		t.Fatalf("tokens=%+v err=%v", rotated, err)
	}
	if _, err := service.Refresh(context.Background(), tokens.RefreshToken); !errors.Is(err, ErrRefreshReuse) {
		t.Fatalf("replay err=%v", err)
	}
	if _, err := service.Refresh(context.Background(), rotated.RefreshToken); !errors.Is(err, ErrInvalidToken) {
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
	signer, _ := NewAccessTokenSigner("modura", "admin", "key-1", []byte(strings.Repeat("k", 32)), time.Minute)
	verifier := NewAccessTokenVerifier("modura", "admin", map[string][]byte{"key-1": []byte(strings.Repeat("k", 32))}, 0)
	service, _ := NewService(store, signer, verifier, passwords, time.Hour, time.Now, func(time.Time) (string, error) { return "id", nil }, func() (string, error) { return strings.Repeat("s", 32), nil })
	for _, test := range []struct{ tenant, login, password string }{{"missing", "alice", "correct horse battery staple"}, {"acme", "alice", "wrong password"}} {
		if _, err := service.Login(context.Background(), test.tenant, test.login, test.password); !errors.Is(err, ErrInvalidCredentials) {
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
	signer, _ := NewAccessTokenSigner("modura", "admin", "key-1", key, 5*time.Minute)
	verifier := NewAccessTokenVerifier("modura", "admin", map[string][]byte{"key-1": key}, 0)
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
	loggedIn, err := service.Login(context.Background(), "acme", "alice", "correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	actor := Actor{TenantID: "tenant-1", UserID: "user-1", SessionID: store.session.ID}
	changed, err := service.ChangePassword(context.Background(), actor, "correct horse battery staple", "a newly secured password", loggedIn.RefreshToken)
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
	signer, _ := NewAccessTokenSigner("modura", "admin", "key-1", key, time.Minute)
	verifier := NewAccessTokenVerifier("modura", "admin", map[string][]byte{"key-1": key}, 0)
	service, err := NewService(store, signer, verifier, passwords, time.Hour, func() time.Time { return now }, func(time.Time) (string, error) { return "token-id", nil }, func() (string, error) { return strings.Repeat("r", 32), nil })
	if err != nil {
		t.Fatal(err)
	}
	secret, err := service.IssueOneTimeToken(context.Background(), "tenant-1", "user-1", PurposePasswordReset, 15*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.ConsumeOneTimeToken(context.Background(), secret, PurposePasswordReset, "a newly recovered password"); err != nil {
		t.Fatal(err)
	}
	if err := service.ConsumeOneTimeToken(context.Background(), secret, PurposePasswordReset, "another secure password"); !errors.Is(err, ErrInvalidToken) {
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
	if err := service.ConsumeOneTimeToken(context.Background(), expiring, PurposePasswordReset, "another secure password"); !errors.Is(err, ErrExpiredToken) {
		t.Fatalf("expired token error = %v", err)
	}
}

func TestDisableAccountIsTenantScopedAndInvalidatesSessions(t *testing.T) {
	service, store := newAdministrativeTestService(t)
	if err := service.DisableAccount(context.Background(), "other-tenant", "user-1", "administrative_disable"); err == nil {
		t.Fatal("cross-tenant disable succeeded")
	}
	if err := service.DisableAccount(context.Background(), "tenant-1", "user-1", "administrative_disable"); err != nil {
		t.Fatal(err)
	}
	if !store.revoked || store.status != "disabled" || store.account.SecurityVersion != 2 {
		t.Fatalf("revoked=%v status=%q version=%d", store.revoked, store.status, store.account.SecurityVersion)
	}
	if err := service.UnlockAccount(context.Background(), "tenant-1", "user-1"); err == nil {
		t.Fatal("administratively disabled account was unlocked")
	}
}

func newAdministrativeTestService(t *testing.T) (*Service, *memoryStore) {
	t.Helper()
	store := &memoryStore{account: Account{TenantID: "tenant-1", UserID: "user-1", SecurityVersion: 1}, status: "active"}
	key := []byte(strings.Repeat("k", 32))
	signer, _ := NewAccessTokenSigner("modura", "admin", "key-1", key, time.Minute)
	verifier := NewAccessTokenVerifier("modura", "admin", map[string][]byte{"key-1": key}, 0)
	service, err := NewService(store, signer, verifier, DefaultPasswordParameters(), time.Hour, time.Now, func(time.Time) (string, error) { return "id", nil }, func() (string, error) { return strings.Repeat("s", 32), nil })
	if err != nil {
		t.Fatal(err)
	}
	return service, store
}
