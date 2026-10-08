// Package postgres persists identity-owned data in PostgreSQL through
// generated, module-private queries.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/modura-dev/modura/backend/internal/modules/identity"
	identitydb "github.com/modura-dev/modura/backend/internal/modules/identity/postgres/db"
)

// Store persists identity data and authentication sessions.
type Store struct{ pool *pgxpool.Pool }

// New constructs a PostgreSQL identity store.
func New(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

// Profile reads an active user only through the authenticated tenant/session tuple.
func (s *Store) Profile(ctx context.Context, actor identity.Actor) (identity.Profile, error) {
	row, err := identitydb.New(s.pool).ProfileBySession(ctx, identitydb.ProfileBySessionParams{TenantID: string(actor.TenantID), ID: string(actor.UserID), ID_2: string(actor.SessionID)})
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.Profile{}, identity.ErrInvalidToken
	}
	if err != nil {
		return identity.Profile{}, fmt.Errorf("read profile: %w", err)
	}
	return identity.Profile{ID: identity.UserID(row.ID), Username: row.Username, Email: pgString(row.Email), Status: row.Status, UpdatedAt: row.UpdatedAt}, nil
}

// UpdateProfile atomically updates self-service fields and returns the
// before and after profile snapshots for transactional audit evidence.
func (s *Store) UpdateProfile(ctx context.Context, tx pgx.Tx, change identity.ProfileChange) (identity.Profile, identity.Profile, error) {
	queries := identitydb.New(tx)
	locked, err := queries.LockSelfProfile(ctx, identitydb.LockSelfProfileParams{TenantID: string(change.Actor.TenantID), ID: string(change.Actor.UserID), ID_2: string(change.Actor.SessionID)})
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.Profile{}, identity.Profile{}, identity.ErrInvalidToken
	}
	if err != nil {
		return identity.Profile{}, identity.Profile{}, fmt.Errorf("lock profile: %w", err)
	}
	before := identity.Profile{ID: change.Actor.UserID, Username: locked.Username, Email: pgString(locked.Email), Status: locked.Status, UpdatedAt: locked.UpdatedAt}
	if err := queries.UpdateSelfProfile(ctx, identitydb.UpdateSelfProfileParams{TenantID: string(change.Actor.TenantID), ID: string(change.Actor.UserID), Username: change.Username, NormalizedUsername: change.NormalizedUsername, Email: pgText(change.Email), NormalizedEmail: pgText(change.NormalizedEmail), UpdatedAt: change.OccurredAt}); err != nil {
		return identity.Profile{}, identity.Profile{}, fmt.Errorf("write profile: %w", err)
	}
	after := identity.Profile{ID: before.ID, Username: change.Username, Email: change.Email, Status: before.Status, UpdatedAt: change.OccurredAt}
	return before, after, nil
}

// pgText converts an optional string to the generated nullable text type.
func pgText(value *string) pgtype.Text {
	if value == nil {
		return pgtype.Text{}
	}
	return pgtype.Text{String: *value, Valid: true}
}

// textValid maps a required string to valid generated text.
func textValid(value string) pgtype.Text {
	return pgtype.Text{String: value, Valid: true}
}

// tsValid maps a required time to valid generated timestamptz.
func tsValid(value time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: value, Valid: true}
}

// textOrEmpty maps an empty string to SQL NULL and other values to valid text.
func textOrEmpty(value string) pgtype.Text {
	if value == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: value, Valid: true}
}

// pgString converts the generated nullable text type to an optional string.
func pgString(value pgtype.Text) *string {
	if !value.Valid {
		return nil
	}
	copied := value.String
	return &copied
}

// pgUUIDOpt converts an optional application identifier to the generated UUID type.
func pgUUIDOpt(value string) pgtype.UUID {
	if value == "" {
		return pgtype.UUID{}
	}
	parsed, err := uuid.Parse(value)
	if err != nil {
		return pgtype.UUID{}
	}
	return pgtype.UUID{Bytes: parsed, Valid: true}
}

// userColumns is the stable non-sensitive projection of a management-visible user.
const userColumns = `id, username, email, status, created_at, updated_at`

func scanUser(row pgx.Row) (identity.TenantUser, error) {
	var user identity.TenantUser
	if err := row.Scan(&user.ID, &user.Username, &user.Email, &user.Status, &user.CreatedAt, &user.UpdatedAt); err != nil {
		return identity.TenantUser{}, err
	}
	return user, nil
}

// ListUsers returns tenant-owned users in stable order without credential data.
func (s *Store) ListUsers(ctx context.Context, tenantID identity.TenantID) ([]identity.TenantUser, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+userColumns+` FROM modura.users WHERE tenant_id = $1 ORDER BY normalized_username, id`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list tenant users: %w", err)
	}
	defer rows.Close()
	users := make([]identity.TenantUser, 0)
	for rows.Next() {
		user, err := scanUser(rows)
		if err != nil {
			return nil, fmt.Errorf("scan tenant user: %w", err)
		}
		users = append(users, user)
	}
	return users, rows.Err()
}

// querier abstracts the row-reading surface shared by the pool and transactions.
type querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// GetUser looks up one user under an explicit tenant scope.
func (s *Store) GetUser(ctx context.Context, tenantID identity.TenantID, userID identity.UserID) (identity.TenantUser, error) {
	return getTenantUser(ctx, s.pool, tenantID, userID)
}

// GetUserTx looks up one user inside the caller's transaction for state snapshots.
func (s *Store) GetUserTx(ctx context.Context, tx pgx.Tx, tenantID identity.TenantID, userID identity.UserID) (identity.TenantUser, error) {
	return getTenantUser(ctx, tx, tenantID, userID)
}

func getTenantUser(ctx context.Context, q querier, tenantID identity.TenantID, userID identity.UserID) (identity.TenantUser, error) {
	user, err := scanUser(q.QueryRow(ctx, `SELECT `+userColumns+` FROM modura.users WHERE tenant_id = $1 AND id = $2`, tenantID, userID))
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.TenantUser{}, identity.ErrUserNotFound
	}
	if err != nil {
		return identity.TenantUser{}, fmt.Errorf("get tenant user: %w", err)
	}
	return user, nil
}

// UserExistsTx reports tenant-local account existence inside the caller's transaction.
func (s *Store) UserExistsTx(ctx context.Context, tx pgx.Tx, tenantID identity.TenantID, userID identity.UserID) (bool, error) {
	present, err := identitydb.New(tx).UserExistsInTenant(ctx, identitydb.UserExistsInTenantParams{TenantID: string(tenantID), ID: string(userID)})
	if err != nil {
		return false, fmt.Errorf("check user existence: %w", err)
	}
	return present, nil
}

// ListTenantSummaries returns platform-visible tenant summaries in stable order.
func (s *Store) ListTenantSummaries(ctx context.Context) ([]identity.TenantSummary, error) {
	rows, err := identitydb.New(s.pool).ListTenantSummaries(ctx)
	if err != nil {
		return nil, fmt.Errorf("list tenants: %w", err)
	}
	summaries := make([]identity.TenantSummary, 0, len(rows))
	for _, row := range rows {
		summaries = append(summaries, identity.TenantSummary{ID: identity.TenantID(row.ID), Slug: row.Slug, DisplayName: row.DisplayName, Status: row.Status, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt})
	}
	return summaries, nil
}

// LockTenantProfile reads tenant profile state with FOR UPDATE inside the caller's transaction.
func (s *Store) LockTenantProfile(ctx context.Context, tx pgx.Tx, tenantID identity.TenantID) (identity.TenantProfileState, error) {
	locked, err := identitydb.New(tx).LockTenantProfile(ctx, string(tenantID))
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.TenantProfileState{}, identity.ErrTenantNotFound
	}
	if err != nil {
		return identity.TenantProfileState{}, fmt.Errorf("lock tenant profile: %w", err)
	}
	return identity.TenantProfileState{Slug: locked.Slug, DisplayName: locked.DisplayName, Status: locked.Status, UpdatedAt: locked.UpdatedAt}, nil
}

// UpdateTenantProfile writes mutable tenant presentation data inside the caller's transaction.
func (s *Store) UpdateTenantProfile(ctx context.Context, tx pgx.Tx, tenantID identity.TenantID, displayName string, now time.Time) error {
	if err := identitydb.New(tx).UpdateTenantProfile(ctx, identitydb.UpdateTenantProfileParams{ID: string(tenantID), DisplayName: displayName, UpdatedAt: now}); err != nil {
		return fmt.Errorf("update tenant profile: %w", err)
	}
	return nil
}

// TransitionTenantStatus applies an exact lifecycle transition inside the caller's transaction.
func (s *Store) TransitionTenantStatus(ctx context.Context, tx pgx.Tx, tenantID identity.TenantID, from, to string, now time.Time) error {
	changed, err := identitydb.New(tx).TransitionTenantStatus(ctx, identitydb.TransitionTenantStatusParams{ID: string(tenantID), Status: from, Status_2: to, UpdatedAt: now})
	if err != nil {
		return fmt.Errorf("transition tenant status: %w", err)
	}
	if changed == 1 {
		return nil
	}
	if _, err := identitydb.New(tx).TenantStatusByID(ctx, string(tenantID)); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return identity.ErrTenantNotFound
		}
		return fmt.Errorf("check tenant status: %w", err)
	}
	return identity.ErrInvalidTenantTransition
}

// LoginGuardLockedUntil returns when a credential-throttling lockout ends.
// A zero time means the login pair is not locked.
func (s *Store) LoginGuardLockedUntil(ctx context.Context, tenantSlug, login string, now time.Time) (time.Time, error) {
	lockedUntil, err := identitydb.New(s.pool).LoginGuardLockedUntil(ctx, identitydb.LoginGuardLockedUntilParams{TenantSlug: tenantSlug, NormalizedLogin: login})
	if errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("read login guard: %w", err)
	}
	if lockedUntil.Valid && lockedUntil.Time.After(now) {
		return lockedUntil.Time, nil
	}
	return time.Time{}, nil
}

// RecordLoginFailure counts a failed credential attempt, enforces the
// throttling window, and reports whether this attempt triggered the lockout.
func (s *Store) RecordLoginFailure(ctx context.Context, tenantSlug, login string, now, windowCutoff, lockUntil time.Time, threshold int32) (bool, bool, error) {
	state, err := identitydb.New(s.pool).RecordLoginFailure(ctx, identitydb.RecordLoginFailureParams{TenantSlug: tenantSlug, NormalizedLogin: login, Now: now, Cutoff: windowCutoff, Threshold: threshold, LockUntil: lockUntil})
	if err != nil {
		return false, false, fmt.Errorf("record login failure: %w", err)
	}
	return state.FailureCount == 1, state.LockedUntil.Valid, nil
}

// ClearLoginGuard resets throttling state after a successful login.
func (s *Store) ClearLoginGuard(ctx context.Context, tenantSlug, login string) error {
	if _, err := identitydb.New(s.pool).ClearLoginGuard(ctx, identitydb.ClearLoginGuardParams{TenantSlug: tenantSlug, NormalizedLogin: login}); err != nil {
		return fmt.Errorf("clear login guard: %w", err)
	}
	return nil
}

// RecordSecurityEvent stores privacy-conscious authentication evidence without
// credentials, login strings, network addresses, or user agents.
func (s *Store) RecordSecurityEvent(ctx context.Context, eventType identity.SecurityEventType, tenantID identity.TenantID, userID identity.UserID, correlationID string, now time.Time) error {
	return s.insertSecurityEvent(ctx, identitydb.New(s.pool), eventType, tenantID, userID, correlationID, now)
}

func (s *Store) insertSecurityEvent(ctx context.Context, queries *identitydb.Queries, eventType identity.SecurityEventType, tenantID identity.TenantID, userID identity.UserID, correlationID string, now time.Time) error {
	if err := queries.InsertAuthSecurityEvent(ctx, identitydb.InsertAuthSecurityEventParams{ID: uuid.NewString(), TenantID: pgUUIDOpt(string(tenantID)), UserID: pgUUIDOpt(string(userID)), EventType: string(eventType), CorrelationID: correlationID, OccurredAt: now}); err != nil {
		return fmt.Errorf("insert auth security event: %w", err)
	}
	return nil
}

// FindActiveAccount resolves an active tenant-local account without leaking misses.
func (s *Store) FindActiveAccount(ctx context.Context, tenantSlug, login string) (identity.Account, error) {
	const query = `
SELECT u.tenant_id, u.id, u.password_hash, u.security_version
FROM modura.users u
JOIN modura.tenants t ON t.id = u.tenant_id
WHERE t.slug = $1 AND t.status = 'active' AND u.status = 'active'
  AND (u.normalized_username = $2 OR (u.normalized_email = $2 AND u.email_verified_at IS NOT NULL))`
	var account identity.Account
	err := s.pool.QueryRow(ctx, query, tenantSlug, login).Scan(&account.TenantID, &account.UserID, &account.PasswordHash, &account.SecurityVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.Account{}, identity.ErrInvalidCredentials
	}
	if err != nil {
		return identity.Account{}, fmt.Errorf("find active account: %w", err)
	}
	return account, nil
}

// UpdatePasswordHash replaces a hash only within its owning active tenant account.
func (s *Store) UpdatePasswordHash(ctx context.Context, tenantID identity.TenantID, userID identity.UserID, hash string) error {
	changed, err := identitydb.New(s.pool).UpdatePasswordHash(ctx, identitydb.UpdatePasswordHashParams{TenantID: string(tenantID), ID: string(userID), PasswordHash: textValid(hash)})
	if err != nil {
		return fmt.Errorf("update password hash: %w", err)
	}
	if changed != 1 {
		return identity.ErrInactiveUser
	}
	return nil
}

// CreateSession persists a new refresh-token family.
func (s *Store) CreateSession(ctx context.Context, session identity.NewSession) error {
	if err := identitydb.New(s.pool).InsertAuthSession(ctx, identitydb.InsertAuthSessionParams{ID: string(session.ID), TenantID: string(session.TenantID), UserID: string(session.UserID), FamilyID: session.FamilyID, RefreshTokenHash: session.RefreshHash[:], SecurityVersion: session.SecurityVersion, CreatedAt: session.CreatedAt, ExpiresAt: session.ExpiresAt}); err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	return nil
}

// RotateSession atomically consumes and replaces a refresh token, recording
// privacy-conscious evidence when replay is detected.
func (s *Store) RotateSession(ctx context.Context, presented, next [32]byte, now, expires time.Time, correlationID string) (identity.Session, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return identity.Session{}, fmt.Errorf("begin refresh rotation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := identitydb.New(tx)

	replay, err := queries.ReplayedTokenFamily(ctx, presented[:])
	if err == nil {
		owner, ownerErr := queries.SessionFamilyOwner(ctx, replay)
		if ownerErr == nil {
			if err := s.insertSecurityEvent(ctx, queries, identity.SecurityEventRefreshReplayDetected, identity.TenantID(owner.TenantID), identity.UserID(owner.UserID), correlationID, now); err != nil {
				return identity.Session{}, err
			}
		} else if !errors.Is(ownerErr, pgx.ErrNoRows) {
			return identity.Session{}, fmt.Errorf("read replayed session family: %w", ownerErr)
		}
		if _, err := queries.RevokeFamilySessions(ctx, identitydb.RevokeFamilySessionsParams{FamilyID: replay, RevokedAt: tsValid(now)}); err != nil {
			return identity.Session{}, fmt.Errorf("revoke replayed token family: %w", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return identity.Session{}, fmt.Errorf("commit replay revocation: %w", err)
		}
		return identity.Session{}, identity.ErrRefreshReuse
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return identity.Session{}, fmt.Errorf("check refresh replay: %w", err)
	}

	current, err := queries.LockCurrentSession(ctx, presented[:])
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.Session{}, identity.ErrInvalidToken
	}
	if err != nil {
		return identity.Session{}, fmt.Errorf("lock refresh session: %w", err)
	}
	if !current.ExpiresAt.After(now) {
		return identity.Session{}, identity.ErrExpiredToken
	}
	if err := queries.RecordRefreshTokenUse(ctx, identitydb.RecordRefreshTokenUseParams{TokenHash: presented[:], SessionID: current.ID, FamilyID: current.FamilyID, ConsumedAt: now}); err != nil {
		return identity.Session{}, fmt.Errorf("record consumed refresh token: %w", err)
	}
	if err := queries.RotateSessionSecret(ctx, identitydb.RotateSessionSecretParams{ID: current.ID, RefreshTokenHash: next[:], LastUsedAt: now, ExpiresAt: expires}); err != nil {
		return identity.Session{}, fmt.Errorf("rotate refresh token: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return identity.Session{}, fmt.Errorf("commit refresh rotation: %w", err)
	}
	return identity.Session{ID: identity.SessionID(current.ID), TenantID: identity.TenantID(current.TenantID), UserID: identity.UserID(current.UserID), SecurityVersion: current.SecurityVersion}, nil
}

// RevokeSession revokes one tenant-bound session and records security evidence.
func (s *Store) RevokeSession(ctx context.Context, tenantID identity.TenantID, userID identity.UserID, sessionID identity.SessionID, reason string, now time.Time, correlationID string) error {
	queries := identitydb.New(s.pool)
	revoked, err := queries.RevokeUserSession(ctx, identitydb.RevokeUserSessionParams{TenantID: string(tenantID), UserID: string(userID), ID: string(sessionID), RevokedAt: tsValid(now), RevocationReason: textValid(reason)})
	if err != nil {
		return fmt.Errorf("revoke session: %w", err)
	}
	if revoked == 0 {
		return identity.ErrInvalidToken
	}
	return s.insertSecurityEvent(ctx, queries, identity.SecurityEventSessionsRevoked, tenantID, userID, correlationID, now)
}

// RevokeOtherSessions revokes all of a user's sessions except the current one.
func (s *Store) RevokeOtherSessions(ctx context.Context, tenantID identity.TenantID, userID identity.UserID, sessionID identity.SessionID, reason string, now time.Time) error {
	if err := identitydb.New(s.pool).RevokeSessionsExcept(ctx, identitydb.RevokeSessionsExceptParams{TenantID: string(tenantID), UserID: string(userID), ID: string(sessionID), RevokedAt: tsValid(now), RevocationReason: textValid(reason)}); err != nil {
		return fmt.Errorf("revoke other sessions: %w", err)
	}
	return nil
}

// RevokeAllSessions revokes every session for a tenant-local user.
func (s *Store) RevokeAllSessions(ctx context.Context, tenantID identity.TenantID, userID identity.UserID, reason string, now time.Time, correlationID string) error {
	queries := identitydb.New(s.pool)
	if err := queries.RevokeAllUserSessions(ctx, identitydb.RevokeAllUserSessionsParams{TenantID: string(tenantID), UserID: string(userID), RevokedAt: tsValid(now), RevocationReason: textValid(reason)}); err != nil {
		return fmt.Errorf("revoke all sessions: %w", err)
	}
	return s.insertSecurityEvent(ctx, queries, identity.SecurityEventSessionsRevoked, tenantID, userID, correlationID, now)
}

// ValidateSession confirms that token claims still refer to active server-side state.
func (s *Store) ValidateSession(ctx context.Context, session identity.Session, now time.Time) error {
	_, err := identitydb.New(s.pool).SessionSecurityActive(ctx, identitydb.SessionSecurityActiveParams{ID: string(session.ID), TenantID: string(session.TenantID), UserID: string(session.UserID), SecurityVersion: session.SecurityVersion, ExpiresAt: now})
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.ErrInvalidToken
	}
	if err != nil {
		return fmt.Errorf("validate session: %w", err)
	}
	return nil
}

// PasswordHash reads the current credential only for the actor's active session.
func (s *Store) PasswordHash(ctx context.Context, actor identity.Actor) (string, error) {
	hash, err := identitydb.New(s.pool).PasswordHashBySession(ctx, identitydb.PasswordHashBySessionParams{TenantID: string(actor.TenantID), ID: string(actor.UserID), ID_2: string(actor.SessionID)})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", identity.ErrInvalidCredentials
	}
	if err != nil {
		return "", fmt.Errorf("read password hash: %w", err)
	}
	if !hash.Valid {
		return "", identity.ErrInvalidCredentials
	}
	return hash.String, nil
}

// ChangePassword atomically updates credentials, rotates the current refresh
// secret, increments security state, revokes other sessions, and records
// security evidence.
func (s *Store) ChangePassword(ctx context.Context, actor identity.Actor, expectedHash, newHash string, presented, next [32]byte, now, expires time.Time, correlationID string) (identity.Session, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return identity.Session{}, fmt.Errorf("begin password change: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := identitydb.New(tx)
	locked, err := queries.LockPasswordChange(ctx, identitydb.LockPasswordChangeParams{ID: string(actor.SessionID), TenantID: string(actor.TenantID), UserID: string(actor.UserID), RefreshTokenHash: presented[:], ExpiresAt: now, PasswordHash: textValid(expectedHash)})
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.Session{}, identity.ErrInvalidToken
	}
	if err != nil {
		return identity.Session{}, fmt.Errorf("lock password change: %w", err)
	}
	securityVersion := locked.SecurityVersion + 1
	if _, err := queries.ApplyPasswordChange(ctx, identitydb.ApplyPasswordChangeParams{TenantID: string(actor.TenantID), ID: string(actor.UserID), PasswordHash: textValid(newHash), SecurityVersion: securityVersion, UpdatedAt: now}); err != nil {
		return identity.Session{}, fmt.Errorf("update password: %w", err)
	}
	if err := queries.RevokeSessionsExcept(ctx, identitydb.RevokeSessionsExceptParams{TenantID: string(actor.TenantID), UserID: string(actor.UserID), ID: string(actor.SessionID), RevokedAt: tsValid(now), RevocationReason: textValid("password_changed")}); err != nil {
		return identity.Session{}, fmt.Errorf("revoke sessions after password change: %w", err)
	}
	if err := queries.RecordRefreshTokenUse(ctx, identitydb.RecordRefreshTokenUseParams{TokenHash: presented[:], SessionID: string(actor.SessionID), FamilyID: locked.FamilyID, ConsumedAt: now}); err != nil {
		return identity.Session{}, fmt.Errorf("consume password-change refresh token: %w", err)
	}
	if err := queries.RotateSessionAfterPasswordChange(ctx, identitydb.RotateSessionAfterPasswordChangeParams{ID: string(actor.SessionID), RefreshTokenHash: next[:], SecurityVersion: securityVersion, LastUsedAt: now, ExpiresAt: expires}); err != nil {
		return identity.Session{}, fmt.Errorf("rotate password-change session: %w", err)
	}
	if err := s.insertSecurityEvent(ctx, queries, identity.SecurityEventPasswordChanged, actor.TenantID, actor.UserID, correlationID, now); err != nil {
		return identity.Session{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return identity.Session{}, fmt.Errorf("commit password change: %w", err)
	}
	return identity.Session{ID: actor.SessionID, TenantID: actor.TenantID, UserID: actor.UserID, SecurityVersion: securityVersion}, nil
}

// CreateOneTimeToken replaces any outstanding token for the same user and purpose.
func (s *Store) CreateOneTimeToken(ctx context.Context, tenantID identity.TenantID, userID identity.UserID, purpose identity.OneTimePurpose, id string, tokenHash [32]byte, now, expires time.Time) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin one-time token creation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := identitydb.New(tx)
	if err := queries.InvalidateOneTimeTokens(ctx, identitydb.InvalidateOneTimeTokensParams{TenantID: string(tenantID), UserID: string(userID), Purpose: string(purpose), ConsumedAt: tsValid(now)}); err != nil {
		return fmt.Errorf("invalidate previous one-time tokens: %w", err)
	}
	inserted, err := queries.InsertOneTimeToken(ctx, identitydb.InsertOneTimeTokenParams{ID: id, TenantID: string(tenantID), ID_2: string(userID), Purpose: string(purpose), TokenHash: tokenHash[:], CreatedAt: now, ExpiresAt: expires})
	if err != nil {
		return fmt.Errorf("insert one-time token: %w", err)
	}
	if inserted != 1 {
		return identity.ErrInactiveUser
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit one-time token creation: %w", err)
	}
	return nil
}

// ConsumeOneTimeToken atomically consumes a token, updates credentials,
// invalidates every existing session for the user, and records security evidence.
func (s *Store) ConsumeOneTimeToken(ctx context.Context, tokenHash [32]byte, purpose identity.OneTimePurpose, passwordHash string, now time.Time, correlationID string) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin one-time token consumption: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := identitydb.New(tx)
	locked, err := queries.LockOneTimeToken(ctx, identitydb.LockOneTimeTokenParams{TokenHash: tokenHash[:], Purpose: string(purpose)})
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.ErrInvalidToken
	}
	if err != nil {
		return fmt.Errorf("lock one-time token: %w", err)
	}
	if !locked.ExpiresAt.After(now) {
		return identity.ErrExpiredToken
	}
	if purpose == identity.PurposeInvitation {
		if err := queries.ApplyInvitationActivation(ctx, identitydb.ApplyInvitationActivationParams{TenantID: locked.TenantID, ID: locked.UserID, PasswordHash: textValid(passwordHash), UpdatedAt: now}); err != nil {
			return fmt.Errorf("update one-time token credential: %w", err)
		}
	} else {
		if err := queries.ApplyPasswordReset(ctx, identitydb.ApplyPasswordResetParams{TenantID: locked.TenantID, ID: locked.UserID, PasswordHash: textValid(passwordHash), UpdatedAt: now}); err != nil {
			return fmt.Errorf("update one-time token credential: %w", err)
		}
	}
	if _, err := queries.ConsumeOneTimeTokenRow(ctx, identitydb.ConsumeOneTimeTokenRowParams{TokenHash: tokenHash[:], ConsumedAt: tsValid(now)}); err != nil {
		return fmt.Errorf("consume one-time token: %w", err)
	}
	if err := queries.RevokeAllUserSessions(ctx, identitydb.RevokeAllUserSessionsParams{TenantID: locked.TenantID, UserID: locked.UserID, RevokedAt: tsValid(now), RevocationReason: textValid(string(purpose))}); err != nil {
		return fmt.Errorf("revoke sessions after one-time token: %w", err)
	}
	if err := s.insertSecurityEvent(ctx, queries, identity.SecurityEventPasswordChanged, identity.TenantID(locked.TenantID), identity.UserID(locked.UserID), correlationID, now); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit one-time token consumption: %w", err)
	}
	return nil
}

// DisableAccount atomically disables a user, advances its security version,
// and revokes all sessions inside the caller's transaction. Disabling is
// idempotent and also cancels outstanding invitations and recovery tokens.
func (s *Store) DisableAccount(ctx context.Context, tx pgx.Tx, tenantID identity.TenantID, userID identity.UserID, reason, correlationID string, now time.Time) error {
	queries := identitydb.New(tx)
	changed, err := queries.DisableTenantUser(ctx, identitydb.DisableTenantUserParams{TenantID: string(tenantID), ID: string(userID), UpdatedAt: now})
	if err != nil {
		return fmt.Errorf("update disabled account: %w", err)
	}
	if changed == 0 {
		if _, err := queries.TenantUserStatusByID(ctx, identitydb.TenantUserStatusByIDParams{TenantID: string(tenantID), ID: string(userID)}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return identity.ErrUserNotFound
			}
			return fmt.Errorf("check disabled account: %w", err)
		}
	}
	if err := queries.RevokeTenantUserSessions(ctx, identitydb.RevokeTenantUserSessionsParams{TenantID: string(tenantID), UserID: string(userID), RevokedAt: pgtype.Timestamptz{Time: now, Valid: true}, RevocationReason: pgtype.Text{String: reason, Valid: true}}); err != nil {
		return fmt.Errorf("revoke disabled account sessions: %w", err)
	}
	if err := queries.ConsumeTenantUserOneTimeTokens(ctx, identitydb.ConsumeTenantUserOneTimeTokensParams{TenantID: string(tenantID), UserID: string(userID), ConsumedAt: pgtype.Timestamptz{Time: now, Valid: true}}); err != nil {
		return fmt.Errorf("consume disabled account tokens: %w", err)
	}
	return s.insertSecurityEvent(ctx, queries, identity.SecurityEventSessionsRevoked, tenantID, userID, correlationID, now)
}

// UnlockAccount restores only abuse-locked users inside the caller's
// transaction; active retries are idempotent and administratively disabled
// users remain disabled.
func (s *Store) UnlockAccount(ctx context.Context, tx pgx.Tx, tenantID identity.TenantID, userID identity.UserID, now time.Time) error {
	changed, err := identitydb.New(tx).UnlockTenantUser(ctx, identitydb.UnlockTenantUserParams{TenantID: string(tenantID), ID: string(userID), UpdatedAt: now})
	if err != nil {
		return fmt.Errorf("unlock account: %w", err)
	}
	if changed == 1 {
		return nil
	}
	status, err := identitydb.New(tx).TenantUserStatusByID(ctx, identitydb.TenantUserStatusByIDParams{TenantID: string(tenantID), ID: string(userID)})
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.ErrUserNotFound
	}
	if err != nil {
		return fmt.Errorf("check unlocked account: %w", err)
	}
	if status == "active" {
		return nil
	}
	return identity.ErrInactiveUser
}

// ProvisionTenant creates identity-owned provisioning records in a workflow transaction.
func (s *Store) ProvisionTenant(ctx context.Context, tx pgx.Tx, provisioning identity.TenantProvisioning) error {
	queries := identitydb.New(tx)
	if err := queries.InsertProvisioningTenant(ctx, identitydb.InsertProvisioningTenantParams{ID: string(provisioning.TenantID), Slug: provisioning.Slug, DisplayName: provisioning.DisplayName, CreatedAt: provisioning.CreatedAt}); err != nil {
		return fmt.Errorf("insert provisioning tenant: %w", err)
	}
	if err := queries.InsertInvitedAdministrator(ctx, identitydb.InsertInvitedAdministratorParams{ID: string(provisioning.AdministratorID), TenantID: string(provisioning.TenantID), Username: provisioning.Username, NormalizedUsername: provisioning.NormalizedUsername, Email: textOrEmpty(provisioning.Email), NormalizedEmail: textOrEmpty(provisioning.NormalizedEmail), CreatedAt: provisioning.CreatedAt}); err != nil {
		return fmt.Errorf("insert invited administrator: %w", err)
	}
	if err := queries.InsertAdministratorInvitation(ctx, identitydb.InsertAdministratorInvitationParams{ID: provisioning.InvitationID, TenantID: string(provisioning.TenantID), UserID: string(provisioning.AdministratorID), TokenHash: provisioning.InvitationHash[:], CreatedAt: provisioning.CreatedAt, ExpiresAt: provisioning.InvitationExpires}); err != nil {
		return fmt.Errorf("insert administrator invitation: %w", err)
	}
	return nil
}

// ActivateTenant transitions only a provisioning tenant to active.
func (s *Store) ActivateTenant(ctx context.Context, tx pgx.Tx, tenantID identity.TenantID, now time.Time) error {
	activated, err := identitydb.New(tx).ActivateProvisioningTenant(ctx, identitydb.ActivateProvisioningTenantParams{ID: string(tenantID), UpdatedAt: now})
	if err != nil {
		return fmt.Errorf("update tenant active: %w", err)
	}
	if activated != 1 {
		return identity.ErrInactiveTenant
	}
	return nil
}
