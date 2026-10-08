package identity

import (
	"context"
	"encoding/json"
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
	RotateSession(context.Context, [32]byte, [32]byte, time.Time, time.Time, string) (Session, error)
	RevokeSession(context.Context, TenantID, UserID, SessionID, string, time.Time, string) error
	RevokeOtherSessions(context.Context, TenantID, UserID, SessionID, string, time.Time) error
	RevokeAllSessions(context.Context, TenantID, UserID, string, time.Time, string) error
	ValidateSession(context.Context, Session, time.Time) error
	PasswordHash(context.Context, Actor) (string, error)
	Profile(context.Context, Actor) (Profile, error)
	ListUsers(context.Context, TenantID) ([]TenantUser, error)
	GetUser(context.Context, TenantID, UserID) (TenantUser, error)
	GetUserTx(context.Context, pgx.Tx, TenantID, UserID) (TenantUser, error)
	UserExistsTx(context.Context, pgx.Tx, TenantID, UserID) (bool, error)
	UpdateProfile(context.Context, pgx.Tx, ProfileChange) (Profile, Profile, error)
	ListTenantSummaries(context.Context) ([]TenantSummary, error)
	LockTenantProfile(context.Context, pgx.Tx, TenantID) (TenantProfileState, error)
	UpdateTenantProfile(context.Context, pgx.Tx, TenantID, string, time.Time) error
	TransitionTenantStatus(context.Context, pgx.Tx, TenantID, string, string, time.Time) error
	ChangePassword(context.Context, Actor, string, string, [32]byte, [32]byte, time.Time, time.Time, string) (Session, error)
	CreateOneTimeToken(context.Context, TenantID, UserID, OneTimePurpose, string, [32]byte, time.Time, time.Time) error
	ConsumeOneTimeToken(context.Context, [32]byte, OneTimePurpose, string, time.Time, string) error
	DisableAccount(context.Context, pgx.Tx, TenantID, UserID, string, string, time.Time) error
	UnlockAccount(context.Context, pgx.Tx, TenantID, UserID, time.Time) error
	LoginGuardLockedUntil(context.Context, string, string, time.Time) (time.Time, error)
	RecordLoginFailure(context.Context, string, string, time.Time, time.Time, time.Time, int32) (bool, bool, error)
	ClearLoginGuard(context.Context, string, string) error
	RecordSecurityEvent(context.Context, SecurityEventType, TenantID, UserID, string, time.Time) error
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

// TenantUser is the non-sensitive management projection of a tenant account.
type TenantUser struct {
	ID        UserID
	Username  string
	Email     *string
	Status    string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// ProfileChange carries a self-service profile update inside the caller's transaction.
type ProfileChange struct {
	Actor              Actor
	Username           string
	NormalizedUsername string
	Email              *string
	NormalizedEmail    *string
	CorrelationID      string
	OccurredAt         time.Time
}

// TenantSummary is the platform-visible projection of a tenant.
type TenantSummary struct {
	ID          TenantID
	Slug        string
	DisplayName string
	Status      string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// TenantProfileState is the locked tenant profile snapshot used for optimistic checks.
type TenantProfileState struct {
	Slug        string
	DisplayName string
	Status      string
	UpdatedAt   time.Time
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

// DisableUser disables a tenant-owned account, revokes its sessions, and
// records audit evidence inside the same transaction. Disabling is idempotent
// for already disabled accounts and also cancels outstanding invitations.
func (s *Service) DisableUser(ctx context.Context, actor Actor, target UserID, reason, correlationID string) (TenantUser, error) {
	if !validActor(actor) || s.userManagement == nil || target == "" {
		return TenantUser{}, fmt.Errorf("invalid account disable request")
	}
	reason = strings.TrimSpace(reason)
	correlationID = strings.TrimSpace(correlationID)
	if reason == "" || len(reason) > 256 || correlationID == "" {
		return TenantUser{}, fmt.Errorf("invalid account disable request")
	}
	if actor.UserID == target {
		return TenantUser{}, fmt.Errorf("a user cannot disable their own account")
	}
	now := s.now().UTC()
	var after TenantUser
	err := s.userManagement.transactions.WithinTransaction(ctx, func(tx pgx.Tx) error {
		before, err := s.store.GetUserTx(ctx, tx, actor.TenantID, target)
		if err != nil {
			return err
		}
		if err := s.store.DisableAccount(ctx, tx, actor.TenantID, target, reason, correlationID, now); err != nil {
			return err
		}
		after, err = s.store.GetUserTx(ctx, tx, actor.TenantID, target)
		if err != nil {
			return err
		}
		return s.recordAccountEvent(ctx, tx, actor, target, "identity.user.disabled", reason, correlationID, now, before, after)
	})
	if err != nil {
		return TenantUser{}, err
	}
	return after, nil
}

// UnlockUser restores a tenant-owned abuse-locked account to active state and
// records audit evidence inside the same transaction. It does not reactivate
// administratively disabled accounts.
func (s *Service) UnlockUser(ctx context.Context, actor Actor, target UserID, correlationID string) (TenantUser, error) {
	if !validActor(actor) || s.userManagement == nil || target == "" {
		return TenantUser{}, fmt.Errorf("invalid account unlock request")
	}
	correlationID = strings.TrimSpace(correlationID)
	if correlationID == "" {
		return TenantUser{}, fmt.Errorf("invalid account unlock request")
	}
	now := s.now().UTC()
	var after TenantUser
	err := s.userManagement.transactions.WithinTransaction(ctx, func(tx pgx.Tx) error {
		before, err := s.store.GetUserTx(ctx, tx, actor.TenantID, target)
		if err != nil {
			return err
		}
		if err := s.store.UnlockAccount(ctx, tx, actor.TenantID, target, now); err != nil {
			return err
		}
		after, err = s.store.GetUserTx(ctx, tx, actor.TenantID, target)
		if err != nil {
			return err
		}
		return s.recordAccountEvent(ctx, tx, actor, target, "identity.user.unlocked", "authorized tenant user unlock", correlationID, now, before, after)
	})
	if err != nil {
		return TenantUser{}, err
	}
	return after, nil
}

// Transactor supplies application-owned transaction boundaries.
type Transactor interface {
	WithinTransaction(context.Context, func(pgx.Tx) error) error
}

// AccountAuditor records account lifecycle evidence inside the caller's transaction.
type AccountAuditor interface {
	RecordAccountEvent(context.Context, pgx.Tx, AccountAuditEvent) error
}

// EnableUserManagement adds tenant account administration use cases to a service.
func (s *Service) EnableUserManagement(transactions Transactor, auditor AccountAuditor) error {
	if transactions == nil || auditor == nil {
		return fmt.Errorf("invalid identity user management configuration")
	}
	s.userManagement = &userManagementDependencies{transactions: transactions, auditor: auditor}
	return nil
}

type userManagementDependencies struct {
	transactions Transactor
	auditor      AccountAuditor
}

func (s *Service) recordAccountEvent(ctx context.Context, tx pgx.Tx, actor Actor, target UserID, action, reason, correlationID string, occurredAt time.Time, before, after any) error {
	beforeJSON, err := json.Marshal(before)
	if err != nil {
		return fmt.Errorf("encode before audit state: %w", err)
	}
	afterJSON, err := json.Marshal(after)
	if err != nil {
		return fmt.Errorf("encode after audit state: %w", err)
	}
	return s.userManagement.auditor.RecordAccountEvent(ctx, tx, AccountAuditEvent{Actor: actor, TargetUserID: target, Action: action, Resource: "user", ResourceID: string(target), Reason: reason, CorrelationID: correlationID, OccurredAt: occurredAt, BeforeState: beforeJSON, AfterState: afterJSON})
}

// UserExists reports tenant-local account existence inside the caller's
// transaction so non-owning modules validate users without touching the table.
func (s *Service) UserExists(ctx context.Context, tx pgx.Tx, tenantID TenantID, userID UserID) (bool, error) {
	if tx == nil || tenantID == "" || userID == "" {
		return false, fmt.Errorf("invalid user existence check")
	}
	return s.store.UserExistsTx(ctx, tx, tenantID, userID)
}

// ListTenantSummaries returns platform-visible tenant summaries through the
// owning module's public API.
func (s *Service) ListTenantSummaries(ctx context.Context) ([]TenantSummary, error) {
	return s.store.ListTenantSummaries(ctx)
}

// LockTenantProfile reads tenant profile state with FOR UPDATE inside the
// caller's transaction.
func (s *Service) LockTenantProfile(ctx context.Context, tx pgx.Tx, tenantID TenantID) (TenantProfileState, error) {
	if tx == nil || tenantID == "" {
		return TenantProfileState{}, fmt.Errorf("invalid tenant profile lock")
	}
	return s.store.LockTenantProfile(ctx, tx, tenantID)
}

// UpdateTenantProfile writes mutable tenant presentation data inside the
// caller's transaction.
func (s *Service) UpdateTenantProfile(ctx context.Context, tx pgx.Tx, tenantID TenantID, displayName string, now time.Time) error {
	if tx == nil || tenantID == "" || displayName == "" || len(displayName) > 128 {
		return fmt.Errorf("invalid tenant profile update")
	}
	return s.store.UpdateTenantProfile(ctx, tx, tenantID, displayName, now.UTC())
}

// TransitionTenantStatus applies an exact lifecycle transition inside the
// caller's transaction.
func (s *Service) TransitionTenantStatus(ctx context.Context, tx pgx.Tx, tenantID TenantID, from, to string, now time.Time) error {
	if tx == nil || tenantID == "" || from == "" || to == "" {
		return fmt.Errorf("invalid tenant status transition")
	}
	return s.store.TransitionTenantStatus(ctx, tx, tenantID, from, to, now.UTC())
}

func validActor(actor Actor) bool {
	return actor.TenantID != "" && actor.UserID != "" && actor.SessionID != ""
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
func (s *Service) ConsumeOneTimeToken(ctx context.Context, token string, purpose OneTimePurpose, newPassword, correlationID string) error {
	if token == "" || (purpose != PurposeInvitation && purpose != PurposePasswordReset) {
		return ErrInvalidToken
	}
	hash, err := HashPassword(newPassword, s.password)
	if err != nil {
		return err
	}
	if err := s.store.ConsumeOneTimeToken(ctx, HashOpaqueToken(token), purpose, hash, s.now().UTC(), strings.TrimSpace(correlationID)); err != nil {
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
	userManagement  *userManagementDependencies
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

// Login throttling parameters: five failed attempts within fifteen minutes
// lock the credential pair for fifteen minutes.
const (
	loginFailureThreshold = 5
	loginFailureWindow    = 15 * time.Minute
	loginLockout          = 15 * time.Minute
)

// Login verifies tenant-scoped credentials, throttles repeated failures, and
// establishes a session.
func (s *Service) Login(ctx context.Context, tenantSlug, login, password, correlationID string) (Tokens, error) {
	slug := NormalizeLogin(tenantSlug)
	norm := NormalizeLogin(login)
	correlationID = strings.TrimSpace(correlationID)
	if slug == "" || norm == "" || password == "" {
		return Tokens{}, ErrInvalidCredentials
	}
	now := s.now().UTC()
	if lockedUntil, err := s.store.LoginGuardLockedUntil(ctx, slug, norm, now); err != nil {
		return Tokens{}, fmt.Errorf("check login throttle: %w", err)
	} else if !lockedUntil.IsZero() {
		return Tokens{}, ErrAccountLocked
	}
	account, err := s.store.FindActiveAccount(ctx, slug, norm)
	if err != nil {
		if errors.Is(err, ErrInvalidCredentials) || errors.Is(err, ErrInactiveTenant) || errors.Is(err, ErrInactiveUser) {
			_, _, _ = VerifyPassword(password, s.dummyHash, s.password)
			s.recordLoginFailure(ctx, slug, norm, correlationID, now)
			return Tokens{}, ErrInvalidCredentials
		}
		return Tokens{}, fmt.Errorf("find login account: %w", err)
	}
	valid, needsRehash, err := VerifyPassword(password, account.PasswordHash, s.password)
	if err != nil || !valid {
		s.recordLoginFailure(ctx, slug, norm, correlationID, now)
		return Tokens{}, ErrInvalidCredentials
	}
	if err := s.store.ClearLoginGuard(ctx, slug, norm); err != nil {
		return Tokens{}, fmt.Errorf("clear login throttle: %w", err)
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

// recordLoginFailure counts one failed attempt and emits privacy-conscious
// security events only at the first failure of a window and at lockout.
func (s *Service) recordLoginFailure(ctx context.Context, slug, login, correlationID string, now time.Time) {
	cutoff := now.Add(-loginFailureWindow)
	first, locked, err := s.store.RecordLoginFailure(ctx, slug, login, now, cutoff, now.Add(loginLockout), loginFailureThreshold)
	if err != nil {
		return
	}
	if first {
		_ = s.store.RecordSecurityEvent(ctx, SecurityEventLoginFailed, "", "", correlationID, now)
	}
	if locked {
		_ = s.store.RecordSecurityEvent(ctx, SecurityEventLoginLocked, "", "", correlationID, now)
	}
}

// Refresh atomically consumes a refresh token, rotates its session secret,
// and records privacy-conscious evidence when replay is detected.
func (s *Service) Refresh(ctx context.Context, refreshToken, correlationID string) (Tokens, error) {
	if refreshToken == "" {
		return Tokens{}, ErrInvalidToken
	}
	nextSecret, err := s.newSecret()
	if err != nil {
		return Tokens{}, fmt.Errorf("generate refresh token: %w", err)
	}
	now := s.now().UTC()
	session, err := s.store.RotateSession(ctx, HashOpaqueToken(refreshToken), HashOpaqueToken(nextSecret), now, now.Add(s.refreshLifetime), strings.TrimSpace(correlationID))
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

// Logout revokes the actor's current session and records security evidence.
func (s *Service) Logout(ctx context.Context, actor Actor, correlationID string) error {
	now := s.now().UTC()
	if err := s.store.RevokeSession(ctx, actor.TenantID, actor.UserID, actor.SessionID, "logout", now, strings.TrimSpace(correlationID)); err != nil {
		return err
	}
	return s.store.RecordSecurityEvent(ctx, SecurityEventSessionsRevoked, actor.TenantID, actor.UserID, strings.TrimSpace(correlationID), now)
}

// Profile returns the current active user's tenant-scoped profile.
func (s *Service) Profile(ctx context.Context, actor Actor) (Profile, error) {
	return s.store.Profile(ctx, actor)
}

// ListUsers returns only users owned by the verified tenant.
func (s *Service) ListUsers(ctx context.Context, tenantID TenantID) ([]TenantUser, error) {
	if tenantID == "" {
		return nil, fmt.Errorf("invalid tenant scope")
	}
	return s.store.ListUsers(ctx, tenantID)
}

// GetUser returns one tenant-owned user without cross-tenant disclosure.
func (s *Service) GetUser(ctx context.Context, tenantID TenantID, userID UserID) (TenantUser, error) {
	if tenantID == "" || userID == "" {
		return TenantUser{}, fmt.Errorf("invalid user scope")
	}
	return s.store.GetUser(ctx, tenantID, userID)
}

// UpdateProfile changes the current user's login profile and records
// transactional audit evidence inside the same database transaction.
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
	if s.userManagement == nil {
		return Profile{}, fmt.Errorf("invalid identity user management configuration")
	}
	now := s.now().UTC()
	change := ProfileChange{Actor: actor, Username: username, NormalizedUsername: NormalizeLogin(username), Email: email, NormalizedEmail: normalizedEmail, CorrelationID: correlationID, OccurredAt: now}
	var after Profile
	err := s.userManagement.transactions.WithinTransaction(ctx, func(tx pgx.Tx) error {
		before, updated, err := s.store.UpdateProfile(ctx, tx, change)
		if err != nil {
			return err
		}
		after = updated
		return s.recordAccountEvent(ctx, tx, actor, actor.UserID, "identity.profile-updated", "self-service profile update", correlationID, now, before, after)
	})
	if err != nil {
		return Profile{}, err
	}
	return after, nil
}

// LogoutAll revokes every session owned by the actor's tenant-local user.
func (s *Service) LogoutAll(ctx context.Context, actor Actor, correlationID string) error {
	now := s.now().UTC()
	if err := s.store.RevokeAllSessions(ctx, actor.TenantID, actor.UserID, "logout_all", now, strings.TrimSpace(correlationID)); err != nil {
		return err
	}
	return s.store.RecordSecurityEvent(ctx, SecurityEventSessionsRevoked, actor.TenantID, actor.UserID, strings.TrimSpace(correlationID), now)
}

// ChangePassword verifies the current password, rotates the current session,
// and revokes every other session atomically with security evidence.
func (s *Service) ChangePassword(ctx context.Context, actor Actor, currentPassword, newPassword, refreshToken, correlationID string) (Tokens, error) {
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
	session, err := s.store.ChangePassword(ctx, actor, storedHash, newHash, HashOpaqueToken(refreshToken), HashOpaqueToken(nextSecret), now, now.Add(s.refreshLifetime), strings.TrimSpace(correlationID))
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
