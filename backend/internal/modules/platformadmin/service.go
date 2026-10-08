// Package platformadmin owns global platform-administrator authentication.
package platformadmin

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/psmMRFP/WhereToLive/backend/internal/modules/identity"
)

var (
	// ErrInvalidCredentials deliberately hides which platform credential failed.
	ErrInvalidCredentials = errors.New("invalid platform credentials")
	// ErrInvalidToken means a platform token is invalid, expired, reused, or revoked.
	ErrInvalidToken = errors.New("invalid platform token")
	// ErrBootstrapComplete means a platform administrator already exists.
	ErrBootstrapComplete = errors.New("platform bootstrap already complete")
)

// AdministratorID identifies a global platform administrator.
type AdministratorID string

// Actor is a verified global platform principal and session.
type Actor struct {
	AdministratorID AdministratorID
	SessionID       identity.SessionID
}

// Account is the credential state required for login.
type Account struct {
	ID              AdministratorID
	PasswordHash    string
	SecurityVersion int64
}

// Session is a server-side platform refresh session.
type Session struct {
	ID              identity.SessionID
	AdministratorID AdministratorID
	SecurityVersion int64
}

// NewSession contains initial platform refresh-session state.
type NewSession struct {
	Session
	FamilyID    string
	RefreshHash [32]byte
	CreatedAt   time.Time
	ExpiresAt   time.Time
}

// Tokens contains platform access and refresh credentials.
type Tokens struct {
	AccessToken      string
	RefreshToken     string
	ExpiresIn        time.Duration
	RefreshExpiresIn time.Duration
}

// Store is the persistence boundary consumed by platform authentication.
type Store interface {
	Bootstrap(context.Context, AdministratorID, string, string, string, time.Time) error
	FindActiveAccount(context.Context, string) (Account, error)
	CreateSession(context.Context, NewSession) error
	RotateSession(context.Context, [32]byte, [32]byte, time.Time, time.Time) (Session, error)
	ValidateSession(context.Context, Session, time.Time) error
	RevokeSession(context.Context, Actor, string, time.Time) error
}

// Service implements platform-administrator bootstrap and authentication.
type Service struct {
	store           Store
	signer          identity.AccessTokenSigner
	verifier        identity.AccessTokenVerifier
	password        identity.PasswordParameters
	refreshLifetime time.Duration
	now             func() time.Time
	newID           func(time.Time) (string, error)
	newSecret       func() (string, error)
	dummyHash       string
}

// NewService constructs a platform-administrator service.
func NewService(store Store, signer identity.AccessTokenSigner, verifier identity.AccessTokenVerifier, password identity.PasswordParameters, refreshLifetime time.Duration, now func() time.Time, newID func(time.Time) (string, error), newSecret func() (string, error)) (*Service, error) {
	if store == nil || refreshLifetime <= 0 || now == nil || newID == nil || newSecret == nil {
		return nil, fmt.Errorf("invalid platform administrator service configuration")
	}
	dummyHash, err := identity.HashPassword("wheretolive platform timing defense", password)
	if err != nil {
		return nil, fmt.Errorf("create platform timing defense: %w", err)
	}
	return &Service{store: store, signer: signer, verifier: verifier, password: password, refreshLifetime: refreshLifetime, now: now, newID: newID, newSecret: newSecret, dummyHash: dummyHash}, nil
}

// Bootstrap creates the first platform administrator and refuses all retries
// after any administrator exists.
func (s *Service) Bootstrap(ctx context.Context, username, password string) (AdministratorID, error) {
	normalized := identity.NormalizeLogin(username)
	if normalized == "" {
		return "", fmt.Errorf("invalid platform administrator username")
	}
	hash, err := identity.HashPassword(password, s.password)
	if err != nil {
		return "", err
	}
	now := s.now().UTC()
	id, err := s.newID(now)
	if err != nil {
		return "", fmt.Errorf("generate platform administrator ID: %w", err)
	}
	if err := s.store.Bootstrap(ctx, AdministratorID(id), username, normalized, hash, now); err != nil {
		return "", err
	}
	return AdministratorID(id), nil
}

// Login establishes a global platform session.
func (s *Service) Login(ctx context.Context, username, password string) (Tokens, error) {
	account, err := s.store.FindActiveAccount(ctx, identity.NormalizeLogin(username))
	if err != nil {
		_, _, _ = identity.VerifyPassword(password, s.dummyHash, s.password)
		return Tokens{}, ErrInvalidCredentials
	}
	valid, _, err := identity.VerifyPassword(password, account.PasswordHash, s.password)
	if err != nil || !valid {
		return Tokens{}, ErrInvalidCredentials
	}
	return s.startSession(ctx, account)
}

// Refresh atomically rotates a platform refresh token.
func (s *Service) Refresh(ctx context.Context, token string) (Tokens, error) {
	if token == "" {
		return Tokens{}, ErrInvalidToken
	}
	next, err := s.newSecret()
	if err != nil {
		return Tokens{}, fmt.Errorf("generate platform refresh token: %w", err)
	}
	now := s.now().UTC()
	session, err := s.store.RotateSession(ctx, identity.HashOpaqueToken(token), identity.HashOpaqueToken(next), now, now.Add(s.refreshLifetime))
	if err != nil {
		return Tokens{}, ErrInvalidToken
	}
	access, err := s.signAccess(session, now)
	if err != nil {
		return Tokens{}, err
	}
	return Tokens{AccessToken: access, RefreshToken: next, ExpiresIn: s.signer.Lifetime(), RefreshExpiresIn: s.refreshLifetime}, nil
}

// AuthenticateAccess validates a platform access token and current session state.
func (s *Service) AuthenticateAccess(ctx context.Context, token string) (Actor, error) {
	claims, err := s.verifier.Verify(token, s.now().UTC())
	if err != nil || claims.PrincipalType != "platform" {
		return Actor{}, ErrInvalidToken
	}
	session := Session{ID: claims.SessionID, AdministratorID: AdministratorID(claims.Subject), SecurityVersion: claims.SecurityVersion}
	if err := s.store.ValidateSession(ctx, session, s.now().UTC()); err != nil {
		return Actor{}, ErrInvalidToken
	}
	return Actor{AdministratorID: session.AdministratorID, SessionID: session.ID}, nil
}

// Logout revokes the current platform-administrator session.
func (s *Service) Logout(ctx context.Context, actor Actor) error {
	if actor.AdministratorID == "" || actor.SessionID == "" {
		return ErrInvalidToken
	}
	if err := s.store.RevokeSession(ctx, actor, "logout", s.now().UTC()); err != nil {
		return fmt.Errorf("logout platform session: %w", err)
	}
	return nil
}

func (s *Service) startSession(ctx context.Context, account Account) (Tokens, error) {
	now := s.now().UTC()
	sessionID, err := s.newID(now)
	if err != nil {
		return Tokens{}, fmt.Errorf("generate platform session ID: %w", err)
	}
	familyID, err := s.newID(now)
	if err != nil {
		return Tokens{}, fmt.Errorf("generate platform family ID: %w", err)
	}
	secret, err := s.newSecret()
	if err != nil {
		return Tokens{}, fmt.Errorf("generate platform refresh token: %w", err)
	}
	session := Session{ID: identity.SessionID(sessionID), AdministratorID: account.ID, SecurityVersion: account.SecurityVersion}
	if err := s.store.CreateSession(ctx, NewSession{Session: session, FamilyID: familyID, RefreshHash: identity.HashOpaqueToken(secret), CreatedAt: now, ExpiresAt: now.Add(s.refreshLifetime)}); err != nil {
		return Tokens{}, fmt.Errorf("create platform session: %w", err)
	}
	access, err := s.signAccess(session, now)
	if err != nil {
		return Tokens{}, err
	}
	return Tokens{AccessToken: access, RefreshToken: secret, ExpiresIn: s.signer.Lifetime(), RefreshExpiresIn: s.refreshLifetime}, nil
}

func (s *Service) signAccess(session Session, now time.Time) (string, error) {
	tokenID, err := s.newID(now)
	if err != nil {
		return "", fmt.Errorf("generate platform access token ID: %w", err)
	}
	token, err := s.signer.SignPlatform(identity.UserID(session.AdministratorID), session.ID, tokenID, session.SecurityVersion, now)
	if err != nil {
		return "", fmt.Errorf("sign platform access token: %w", err)
	}
	return token, nil
}
