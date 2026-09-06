package identity

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Account is the credential state needed to establish a session.
type Account struct {
	TenantID        TenantID
	UserID          UserID
	PasswordHash    string
	SecurityVersion int64
}

// Session is the verified state bound into an access token.
type Session struct {
	ID              SessionID
	TenantID        TenantID
	UserID          UserID
	SecurityVersion int64
}

// NewSession contains the state needed for an initial refresh session.
type NewSession struct {
	Session
	FamilyID    string
	RefreshHash [32]byte
	CreatedAt   time.Time
	ExpiresAt   time.Time
}

// Store is the atomic persistence boundary required by authentication use cases.
// Implementations must map every lookup to an active tenant and tenant-owned user.
type Store interface {
	FindActiveAccount(context.Context, string, string) (Account, error)
	UpdatePasswordHash(context.Context, TenantID, UserID, string) error
	CreateSession(context.Context, NewSession) error
	RotateSession(context.Context, [32]byte, [32]byte, time.Time, time.Time) (Session, error)
	RevokeSession(context.Context, TenantID, UserID, SessionID, string, time.Time) error
	RevokeOtherSessions(context.Context, TenantID, UserID, SessionID, string, time.Time) error
	RevokeAllSessions(context.Context, TenantID, UserID, string, time.Time) error
	ValidateSession(context.Context, Session, time.Time) error
	PasswordHash(context.Context, Actor) (string, error)
	Profile(context.Context, Actor) (Profile, error)
	UpdateProfile(context.Context, ProfileChange) (Profile, error)
	ChangePassword(context.Context, Actor, string, string, [32]byte, [32]byte, time.Time, time.Time) (Session, error)
	CreateOneTimeToken(context.Context, TenantID, UserID, OneTimePurpose, string, [32]byte, time.Time, time.Time) error
	ConsumeOneTimeToken(context.Context, [32]byte, OneTimePurpose, string, time.Time) error
	DisableAccount(context.Context, TenantID, UserID, string, time.Time) error
	UnlockAccount(context.Context, TenantID, UserID, time.Time) error
	ProvisionTenant(context.Context, pgx.Tx, TenantProvisioning) error
	ActivateTenant(context.Context, pgx.Tx, TenantID, time.Time) error
}

// Profile is the authenticated user's self-service projection.
type Profile struct {
	ID        UserID
	Username  string
	Email     *string
	Status    string
	UpdatedAt time.Time
}

// ProfileChange carries an audited self-service profile update.
type ProfileChange struct {
	Actor              Actor
	Username           string
	NormalizedUsername string
	Email              *string
	NormalizedEmail    *string
	CorrelationID      string
	AuditID            string
	OccurredAt         time.Time
}

// TenantProvisioning contains identity-owned state for atomic tenant setup.
type TenantProvisioning struct {
	TenantID           TenantID
	Slug               string
	DisplayName        string
	AdministratorID    UserID
	Username           string
	NormalizedUsername string
	Email              string
	NormalizedEmail    string
	InvitationID       string
	InvitationHash     [32]byte
	CreatedAt          time.Time
	InvitationExpires  time.Time
}

// ProvisionTenant creates a provisioning tenant, its invited administrator,
// and the invitation token inside the caller-owned transaction.
func (s *Service) ProvisionTenant(ctx context.Context, tx pgx.Tx, provisioning TenantProvisioning) error {
	if tx == nil || provisioning.TenantID == "" || provisioning.AdministratorID == "" || provisioning.Slug == "" || provisioning.NormalizedUsername == "" {
		return fmt.Errorf("invalid tenant provisioning identity")
	}
	if err := s.store.ProvisionTenant(ctx, tx, provisioning); err != nil {
		return fmt.Errorf("provision tenant identity: %w", err)
	}
	return nil
}

// ActivateTenant marks a fully provisioned tenant active in the shared transaction.
func (s *Service) ActivateTenant(ctx context.Context, tx pgx.Tx, tenantID TenantID, now time.Time) error {
	if err := s.store.ActivateTenant(ctx, tx, tenantID, now); err != nil {
		return fmt.Errorf("activate tenant: %w", err)
	}
	return nil
}

// DisableAccount disables a tenant-owned user and invalidates every session.
// The caller is responsible for completing authorization before invoking this
// application operation; it is intentionally not exposed as an HTTP endpoint.
func (s *Service) DisableAccount(ctx context.Context, tenantID TenantID, userID UserID, reason string) error {
	if tenantID == "" || userID == "" || reason == "" {
		return fmt.Errorf("invalid account disable request")
	}
	if err := s.store.DisableAccount(ctx, tenantID, userID, reason, s.now().UTC()); err != nil {
		return fmt.Errorf("disable account: %w", err)
	}
	return nil
}

// UnlockAccount restores a tenant-owned abuse-locked user to active state.
// It does not reactivate administratively disabled users.
func (s *Service) UnlockAccount(ctx context.Context, tenantID TenantID, userID UserID) error {
	if tenantID == "" || userID == "" {
		return fmt.Errorf("invalid account unlock request")
	}
	if err := s.store.UnlockAccount(ctx, tenantID, userID, s.now().UTC()); err != nil {
		return fmt.Errorf("unlock account: %w", err)
	}
	return nil
}

// IssueOneTimeToken creates a token for a trusted invitation or recovery
// coordinator. The returned secret must be delivered without logging it.
func (s *Service) IssueOneTimeToken(ctx context.Context, tenantID TenantID, userID UserID, purpose OneTimePurpose, lifetime time.Duration) (string, error) {
	if tenantID == "" || userID == "" || (purpose != PurposeInvitation && purpose != PurposePasswordReset) || lifetime <= 0 {
		return "", fmt.Errorf("invalid one-time token request")
	}
	id, err := s.newID(s.now().UTC())
	if err != nil {
		return "", fmt.Errorf("generate one-time token ID: %w", err)
	}
	secret, err := s.newSecret()
	if err != nil {
		return "", fmt.Errorf("generate one-time token: %w", err)
	}
	now := s.now().UTC()
	if err := s.store.CreateOneTimeToken(ctx, tenantID, userID, purpose, id, HashOpaqueToken(secret), now, now.Add(lifetime)); err != nil {
		return "", fmt.Errorf("create one-time token: %w", err)
	}
	return secret, nil
}

// ConsumeOneTimeToken atomically establishes or replaces a password and
// invalidates all existing sessions.
func (s *Service) ConsumeOneTimeToken(ctx context.Context, token string, purpose OneTimePurpose, newPassword string) error {
	if token == "" || (purpose != PurposeInvitation && purpose != PurposePasswordReset) {
		return ErrInvalidToken
	}
	hash, err := HashPassword(newPassword, s.password)
	if err != nil {
		return err
	}
	if err := s.store.ConsumeOneTimeToken(ctx, HashOpaqueToken(token), purpose, hash, s.now().UTC()); err != nil {
		if errors.Is(err, ErrInvalidToken) || errors.Is(err, ErrExpiredToken) {
			return err
		}
		return fmt.Errorf("consume one-time token: %w", err)
	}
	return nil
}

// Tokens contains a short-lived access token and rotating refresh secret.
type Tokens struct {
	AccessToken      string
	RefreshToken     string
	ExpiresIn        time.Duration
	RefreshExpiresIn time.Duration
}

// Service implements local authentication use cases.
type Service struct {
	store           Store
	signer          AccessTokenSigner
	verifier        AccessTokenVerifier
	password        PasswordParameters
	refreshLifetime time.Duration
	now             func() time.Time
	newID           func(time.Time) (string, error)
	newSecret       func() (string, error)
	dummyHash       string
}

// NewService constructs an identity service with explicit clock and entropy dependencies.
func NewService(store Store, signer AccessTokenSigner, verifier AccessTokenVerifier, password PasswordParameters, refreshLifetime time.Duration, now func() time.Time, newID func(time.Time) (string, error), newSecret func() (string, error)) (*Service, error) {
	if store == nil || refreshLifetime <= 0 || now == nil || newID == nil || newSecret == nil {
		return nil, fmt.Errorf("invalid identity service configuration")
	}
	dummyHash, err := HashPassword("modura timing defense password", password)
	if err != nil {
		return nil, fmt.Errorf("create credential timing defense: %w", err)
	}
	return &Service{store: store, signer: signer, verifier: verifier, password: password, refreshLifetime: refreshLifetime, now: now, newID: newID, newSecret: newSecret, dummyHash: dummyHash}, nil
}

// AuthenticateAccess validates a bearer token and its current server-side security state.
func (s *Service) AuthenticateAccess(ctx context.Context, token string) (Actor, error) {
	claims, err := s.verifier.Verify(token, s.now().UTC())
	if err != nil {
		return Actor{}, ErrInvalidToken
	}
	session := Session{ID: claims.SessionID, TenantID: claims.TenantID, UserID: claims.Subject, SecurityVersion: claims.SecurityVersion}
	if err := s.store.ValidateSession(ctx, session, s.now().UTC()); err != nil {
		return Actor{}, ErrInvalidToken
	}
	return Actor{TenantID: claims.TenantID, UserID: claims.Subject, SessionID: claims.SessionID}, nil
}

// Login verifies tenant-scoped credentials and establishes a session.
func (s *Service) Login(ctx context.Context, tenantSlug, login, password string) (Tokens, error) {
	account, err := s.store.FindActiveAccount(ctx, NormalizeLogin(tenantSlug), NormalizeLogin(login))
	if err != nil {
		if errors.Is(err, ErrInvalidCredentials) || errors.Is(err, ErrInactiveTenant) || errors.Is(err, ErrInactiveUser) {
			_, _, _ = VerifyPassword(password, s.dummyHash, s.password)
			return Tokens{}, ErrInvalidCredentials
		}
		return Tokens{}, fmt.Errorf("find login account: %w", err)
	}
	valid, needsRehash, err := VerifyPassword(password, account.PasswordHash, s.password)
	if err != nil || !valid {
		return Tokens{}, ErrInvalidCredentials
	}
	if needsRehash {
		hash, hashErr := HashPassword(password, s.password)
		if hashErr != nil {
			return Tokens{}, fmt.Errorf("rehash password: %w", hashErr)
		}
		if err := s.store.UpdatePasswordHash(ctx, account.TenantID, account.UserID, hash); err != nil {
			return Tokens{}, fmt.Errorf("store password rehash: %w", err)
		}
	}
	return s.startSession(ctx, account)
}

// Refresh atomically consumes a refresh token and rotates its session secret.
func (s *Service) Refresh(ctx context.Context, refreshToken string) (Tokens, error) {
	if refreshToken == "" {
		return Tokens{}, ErrInvalidToken
	}
	nextSecret, err := s.newSecret()
	if err != nil {
		return Tokens{}, fmt.Errorf("generate refresh token: %w", err)
	}
	now := s.now().UTC()
	session, err := s.store.RotateSession(ctx, HashOpaqueToken(refreshToken), HashOpaqueToken(nextSecret), now, now.Add(s.refreshLifetime))
	if err != nil {
		if errors.Is(err, ErrRefreshReuse) || errors.Is(err, ErrInvalidToken) || errors.Is(err, ErrExpiredToken) {
			return Tokens{}, err
		}
		return Tokens{}, fmt.Errorf("rotate refresh session: %w", err)
	}
	access, err := s.signAccess(session, now)
	if err != nil {
		return Tokens{}, err
	}
	return Tokens{AccessToken: access, RefreshToken: nextSecret, ExpiresIn: s.signer.lifetime, RefreshExpiresIn: s.refreshLifetime}, nil
}

// Logout revokes the actor's current session.
func (s *Service) Logout(ctx context.Context, actor Actor) error {
	return s.store.RevokeSession(ctx, actor.TenantID, actor.UserID, actor.SessionID, "logout", s.now().UTC())
}

// Profile returns the current active user's tenant-scoped profile.
func (s *Service) Profile(ctx context.Context, actor Actor) (Profile, error) {
	return s.store.Profile(ctx, actor)
}

// UpdateProfile changes the current user's login profile and records the write.
func (s *Service) UpdateProfile(ctx context.Context, actor Actor, username string, email *string, correlationID string) (Profile, error) {
	username = strings.TrimSpace(username)
	correlationID = strings.TrimSpace(correlationID)
	if actor.TenantID == "" || actor.UserID == "" || actor.SessionID == "" || username == "" || len(username) > 128 || correlationID == "" {
		return Profile{}, fmt.Errorf("invalid profile update")
	}
	var normalizedEmail *string
	if email != nil {
		value := strings.TrimSpace(*email)
		if value == "" || len(value) > 254 || !strings.Contains(value, "@") {
			return Profile{}, fmt.Errorf("invalid profile email")
		}
		email = &value
		normalized := NormalizeLogin(value)
		normalizedEmail = &normalized
	}
	now := s.now().UTC()
	auditID, err := s.newID(now)
	if err != nil {
		return Profile{}, fmt.Errorf("generate audit ID: %w", err)
	}
	profile, err := s.store.UpdateProfile(ctx, ProfileChange{Actor: actor, Username: username, NormalizedUsername: NormalizeLogin(username), Email: email, NormalizedEmail: normalizedEmail, CorrelationID: correlationID, AuditID: auditID, OccurredAt: now})
	if err != nil {
		return Profile{}, fmt.Errorf("update profile: %w", err)
	}
	return profile, nil
}

// LogoutAll revokes every session owned by the actor's tenant-local user.
func (s *Service) LogoutAll(ctx context.Context, actor Actor) error {
	return s.store.RevokeAllSessions(ctx, actor.TenantID, actor.UserID, "logout_all", s.now().UTC())
}

// ChangePassword verifies the current password, rotates the current session,
// and revokes every other session atomically.
func (s *Service) ChangePassword(ctx context.Context, actor Actor, currentPassword, newPassword, refreshToken string) (Tokens, error) {
	storedHash, err := s.store.PasswordHash(ctx, actor)
	if err != nil {
		return Tokens{}, ErrInvalidCredentials
	}
	valid, _, err := VerifyPassword(currentPassword, storedHash, s.password)
	if err != nil || !valid {
		return Tokens{}, ErrInvalidCredentials
	}
	newHash, err := HashPassword(newPassword, s.password)
	if err != nil {
		return Tokens{}, err
	}
	nextSecret, err := s.newSecret()
	if err != nil {
		return Tokens{}, fmt.Errorf("generate refresh token: %w", err)
	}
	now := s.now().UTC()
	session, err := s.store.ChangePassword(ctx, actor, storedHash, newHash, HashOpaqueToken(refreshToken), HashOpaqueToken(nextSecret), now, now.Add(s.refreshLifetime))
	if err != nil {
		if errors.Is(err, ErrInvalidCredentials) || errors.Is(err, ErrInvalidToken) {
			return Tokens{}, err
		}
		return Tokens{}, fmt.Errorf("change password: %w", err)
	}
	access, err := s.signAccess(session, now)
	if err != nil {
		return Tokens{}, err
	}
	return Tokens{AccessToken: access, RefreshToken: nextSecret, ExpiresIn: s.signer.lifetime, RefreshExpiresIn: s.refreshLifetime}, nil
}

func (s *Service) startSession(ctx context.Context, account Account) (Tokens, error) {
	now := s.now().UTC()
	sessionID, err := s.newID(now)
	if err != nil {
		return Tokens{}, fmt.Errorf("generate session ID: %w", err)
	}
	familyID, err := s.newID(now)
	if err != nil {
		return Tokens{}, fmt.Errorf("generate session family ID: %w", err)
	}
	secret, err := s.newSecret()
	if err != nil {
		return Tokens{}, fmt.Errorf("generate refresh token: %w", err)
	}
	session := Session{ID: SessionID(sessionID), TenantID: account.TenantID, UserID: account.UserID, SecurityVersion: account.SecurityVersion}
	if err := s.store.CreateSession(ctx, NewSession{Session: session, FamilyID: familyID, RefreshHash: HashOpaqueToken(secret), CreatedAt: now, ExpiresAt: now.Add(s.refreshLifetime)}); err != nil {
		return Tokens{}, fmt.Errorf("create refresh session: %w", err)
	}
	access, err := s.signAccess(session, now)
	if err != nil {
		return Tokens{}, err
	}
	return Tokens{AccessToken: access, RefreshToken: secret, ExpiresIn: s.signer.lifetime, RefreshExpiresIn: s.refreshLifetime}, nil
}

func (s *Service) signAccess(session Session, now time.Time) (string, error) {
	tokenID, err := s.newID(now)
	if err != nil {
		return "", fmt.Errorf("generate access token ID: %w", err)
	}
	value, err := s.signer.Sign(Actor{TenantID: session.TenantID, UserID: session.UserID, SessionID: session.ID}, tokenID, session.SecurityVersion, now)
	if err != nil {
		return "", fmt.Errorf("sign access token: %w", err)
	}
	return value, nil
}
