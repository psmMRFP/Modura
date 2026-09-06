// Package postgres persists identity-owned data in PostgreSQL.
package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/modura-dev/modura/backend/internal/modules/identity"
)

// Profile reads an active user only through the authenticated tenant/session tuple.
func (s *Store) Profile(ctx context.Context, actor identity.Actor) (identity.Profile, error) {
	var profile identity.Profile
	err := s.pool.QueryRow(ctx, `SELECT u.id, u.username, u.email, u.status, u.updated_at FROM modura.users u JOIN modura.auth_sessions s ON s.tenant_id = u.tenant_id AND s.user_id = u.id WHERE u.tenant_id = $1 AND u.id = $2 AND s.id = $3 AND u.status = 'active' AND s.revoked_at IS NULL`, actor.TenantID, actor.UserID, actor.SessionID).Scan(&profile.ID, &profile.Username, &profile.Email, &profile.Status, &profile.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.Profile{}, identity.ErrInvalidToken
	}
	if err != nil {
		return identity.Profile{}, fmt.Errorf("read profile: %w", err)
	}
	return profile, nil
}

// UpdateProfile atomically updates self-service fields and records audit snapshots.
func (s *Store) UpdateProfile(ctx context.Context, change identity.ProfileChange) (identity.Profile, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return identity.Profile{}, fmt.Errorf("begin profile update: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var before identity.Profile
	err = tx.QueryRow(ctx, `SELECT u.id, u.username, u.email, u.status, u.updated_at FROM modura.users u JOIN modura.auth_sessions s ON s.tenant_id = u.tenant_id AND s.user_id = u.id WHERE u.tenant_id = $1 AND u.id = $2 AND s.id = $3 AND u.status = 'active' AND s.revoked_at IS NULL FOR UPDATE OF u`, change.Actor.TenantID, change.Actor.UserID, change.Actor.SessionID).Scan(&before.ID, &before.Username, &before.Email, &before.Status, &before.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.Profile{}, identity.ErrInvalidToken
	}
	if err != nil {
		return identity.Profile{}, fmt.Errorf("lock profile: %w", err)
	}
	_, err = tx.Exec(ctx, `UPDATE modura.users SET username = $3, normalized_username = $4, email = $5, normalized_email = $6, email_verified_at = CASE WHEN normalized_email IS NOT DISTINCT FROM $6 THEN email_verified_at ELSE NULL END, updated_at = $7 WHERE tenant_id = $1 AND id = $2`, change.Actor.TenantID, change.Actor.UserID, change.Username, change.NormalizedUsername, change.Email, change.NormalizedEmail, change.OccurredAt)
	if err != nil {
		return identity.Profile{}, fmt.Errorf("write profile: %w", err)
	}
	after := identity.Profile{ID: before.ID, Username: change.Username, Email: change.Email, Status: before.Status, UpdatedAt: change.OccurredAt}
	beforeJSON, _ := json.Marshal(before)
	afterJSON, _ := json.Marshal(after)
	_, err = tx.Exec(ctx, `INSERT INTO modura.audit_events (id, actor_type, actor_id, tenant_id, action, resource, resource_id, reason, result, correlation_id, occurred_at, before_state, after_state) VALUES ($1, 'tenant_user', $2, $3, 'identity.profile-updated', 'user', $2, 'self-service profile update', 'succeeded', $4, $5, $6::jsonb, $7::jsonb)`, change.AuditID, change.Actor.UserID, change.Actor.TenantID, change.CorrelationID, change.OccurredAt, beforeJSON, afterJSON)
	if err != nil {
		return identity.Profile{}, fmt.Errorf("audit profile update: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return identity.Profile{}, fmt.Errorf("commit profile update: %w", err)
	}
	return after, nil
}

// Store persists identity data and authentication sessions.
type Store struct{ pool *pgxpool.Pool }

// New constructs a PostgreSQL identity store.
func New(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

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
	command, err := s.pool.Exec(ctx, `UPDATE modura.users SET password_hash = $3, updated_at = now() WHERE tenant_id = $1 AND id = $2 AND status = 'active'`, tenantID, userID, hash)
	if err != nil {
		return fmt.Errorf("update password hash: %w", err)
	}
	if command.RowsAffected() != 1 {
		return identity.ErrInactiveUser
	}
	return nil
}

// CreateSession persists a new refresh-token family.
func (s *Store) CreateSession(ctx context.Context, session identity.NewSession) error {
	_, err := s.pool.Exec(ctx, `
INSERT INTO modura.auth_sessions
    (id, tenant_id, user_id, family_id, refresh_token_hash, security_version, created_at, last_used_at, expires_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $7, $8)`, session.ID, session.TenantID, session.UserID, session.FamilyID, session.RefreshHash[:], session.SecurityVersion, session.CreatedAt, session.ExpiresAt)
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	return nil
}

// RotateSession atomically consumes and replaces a refresh token.
func (s *Store) RotateSession(ctx context.Context, presented, next [32]byte, now, expires time.Time) (identity.Session, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return identity.Session{}, fmt.Errorf("begin refresh rotation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var replayFamily string
	err = tx.QueryRow(ctx, `SELECT family_id FROM modura.auth_refresh_token_uses WHERE token_hash = $1`, presented[:]).Scan(&replayFamily)
	if err == nil {
		if _, err = tx.Exec(ctx, `UPDATE modura.auth_sessions SET revoked_at = $2, revocation_reason = 'refresh_reuse' WHERE family_id = $1 AND revoked_at IS NULL`, replayFamily, now); err != nil {
			return identity.Session{}, fmt.Errorf("revoke replayed token family: %w", err)
		}
		if err = tx.Commit(ctx); err != nil {
			return identity.Session{}, fmt.Errorf("commit replay revocation: %w", err)
		}
		return identity.Session{}, identity.ErrRefreshReuse
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return identity.Session{}, fmt.Errorf("check refresh replay: %w", err)
	}

	var session identity.Session
	var familyID string
	var expiresAt time.Time
	const selectCurrent = `
SELECT s.id, s.tenant_id, s.user_id, s.security_version, s.family_id, s.expires_at
FROM modura.auth_sessions s
JOIN modura.users u ON u.tenant_id = s.tenant_id AND u.id = s.user_id
JOIN modura.tenants t ON t.id = s.tenant_id
WHERE s.refresh_token_hash = $1 AND s.revoked_at IS NULL
  AND u.status = 'active' AND u.security_version = s.security_version AND t.status = 'active'
FOR UPDATE OF s`
	err = tx.QueryRow(ctx, selectCurrent, presented[:]).Scan(&session.ID, &session.TenantID, &session.UserID, &session.SecurityVersion, &familyID, &expiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.Session{}, identity.ErrInvalidToken
	}
	if err != nil {
		return identity.Session{}, fmt.Errorf("lock refresh session: %w", err)
	}
	if !expiresAt.After(now) {
		return identity.Session{}, identity.ErrExpiredToken
	}
	if _, err = tx.Exec(ctx, `INSERT INTO modura.auth_refresh_token_uses (token_hash, session_id, family_id, consumed_at) VALUES ($1, $2, $3, $4)`, presented[:], session.ID, familyID, now); err != nil {
		return identity.Session{}, fmt.Errorf("record consumed refresh token: %w", err)
	}
	if _, err = tx.Exec(ctx, `UPDATE modura.auth_sessions SET refresh_token_hash = $2, last_used_at = $3, expires_at = $4 WHERE id = $1`, session.ID, next[:], now, expires); err != nil {
		return identity.Session{}, fmt.Errorf("rotate refresh token: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return identity.Session{}, fmt.Errorf("commit refresh rotation: %w", err)
	}
	return session, nil
}

// RevokeSession revokes one tenant-bound session.
func (s *Store) RevokeSession(ctx context.Context, tenantID identity.TenantID, userID identity.UserID, sessionID identity.SessionID, reason string, now time.Time) error {
	return s.revoke(ctx, `UPDATE modura.auth_sessions SET revoked_at = $4, revocation_reason = $5 WHERE tenant_id = $1 AND user_id = $2 AND id = $3 AND revoked_at IS NULL`, tenantID, userID, sessionID, now, reason)
}

// RevokeOtherSessions revokes all of a user's sessions except the current one.
func (s *Store) RevokeOtherSessions(ctx context.Context, tenantID identity.TenantID, userID identity.UserID, sessionID identity.SessionID, reason string, now time.Time) error {
	_, err := s.pool.Exec(ctx, `UPDATE modura.auth_sessions SET revoked_at = $4, revocation_reason = $5 WHERE tenant_id = $1 AND user_id = $2 AND id <> $3 AND revoked_at IS NULL`, tenantID, userID, sessionID, now, reason)
	if err != nil {
		return fmt.Errorf("revoke other sessions: %w", err)
	}
	return nil
}

// RevokeAllSessions revokes every session for a tenant-local user.
func (s *Store) RevokeAllSessions(ctx context.Context, tenantID identity.TenantID, userID identity.UserID, reason string, now time.Time) error {
	_, err := s.pool.Exec(ctx, `UPDATE modura.auth_sessions SET revoked_at = $3, revocation_reason = $4 WHERE tenant_id = $1 AND user_id = $2 AND revoked_at IS NULL`, tenantID, userID, now, reason)
	if err != nil {
		return fmt.Errorf("revoke all sessions: %w", err)
	}
	return nil
}

// ValidateSession confirms that token claims still refer to active server-side state.
func (s *Store) ValidateSession(ctx context.Context, session identity.Session, now time.Time) error {
	const query = `
SELECT 1
FROM modura.auth_sessions s
JOIN modura.users u ON u.tenant_id = s.tenant_id AND u.id = s.user_id
JOIN modura.tenants t ON t.id = s.tenant_id
WHERE s.id = $1 AND s.tenant_id = $2 AND s.user_id = $3
  AND s.security_version = $4 AND u.security_version = $4
  AND s.revoked_at IS NULL AND s.expires_at > $5
  AND u.status = 'active' AND t.status = 'active'`
	var exists int
	if err := s.pool.QueryRow(ctx, query, session.ID, session.TenantID, session.UserID, session.SecurityVersion, now).Scan(&exists); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return identity.ErrInvalidToken
		}
		return fmt.Errorf("validate session: %w", err)
	}
	return nil
}

// PasswordHash reads the current credential only for the actor's active session.
func (s *Store) PasswordHash(ctx context.Context, actor identity.Actor) (string, error) {
	const query = `
SELECT u.password_hash
FROM modura.users u
JOIN modura.auth_sessions s ON s.tenant_id = u.tenant_id AND s.user_id = u.id
JOIN modura.tenants t ON t.id = u.tenant_id
WHERE u.tenant_id = $1 AND u.id = $2 AND s.id = $3
  AND u.status = 'active' AND t.status = 'active' AND s.revoked_at IS NULL`
	var hash string
	if err := s.pool.QueryRow(ctx, query, actor.TenantID, actor.UserID, actor.SessionID).Scan(&hash); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", identity.ErrInvalidCredentials
		}
		return "", fmt.Errorf("read password hash: %w", err)
	}
	return hash, nil
}

// ChangePassword atomically updates credentials, rotates the current refresh
// secret, increments security state, and revokes other sessions.
func (s *Store) ChangePassword(ctx context.Context, actor identity.Actor, expectedHash, newHash string, presented, next [32]byte, now, expires time.Time) (identity.Session, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return identity.Session{}, fmt.Errorf("begin password change: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var familyID string
	var securityVersion int64
	const lock = `
SELECT s.family_id, u.security_version
FROM modura.auth_sessions s
JOIN modura.users u ON u.tenant_id = s.tenant_id AND u.id = s.user_id
JOIN modura.tenants t ON t.id = s.tenant_id
WHERE s.id = $1 AND s.tenant_id = $2 AND s.user_id = $3
  AND s.refresh_token_hash = $4 AND s.revoked_at IS NULL AND s.expires_at > $5
  AND u.password_hash = $6 AND u.status = 'active' AND t.status = 'active'
FOR UPDATE OF s, u`
	err = tx.QueryRow(ctx, lock, actor.SessionID, actor.TenantID, actor.UserID, presented[:], now, expectedHash).Scan(&familyID, &securityVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.Session{}, identity.ErrInvalidToken
	}
	if err != nil {
		return identity.Session{}, fmt.Errorf("lock password change: %w", err)
	}
	securityVersion++
	if _, err = tx.Exec(ctx, `UPDATE modura.users SET password_hash = $3, security_version = $4, updated_at = $5 WHERE tenant_id = $1 AND id = $2`, actor.TenantID, actor.UserID, newHash, securityVersion, now); err != nil {
		return identity.Session{}, fmt.Errorf("update password: %w", err)
	}
	if _, err = tx.Exec(ctx, `UPDATE modura.auth_sessions SET revoked_at = $4, revocation_reason = 'password_changed' WHERE tenant_id = $1 AND user_id = $2 AND id <> $3 AND revoked_at IS NULL`, actor.TenantID, actor.UserID, actor.SessionID, now); err != nil {
		return identity.Session{}, fmt.Errorf("revoke sessions after password change: %w", err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO modura.auth_refresh_token_uses (token_hash, session_id, family_id, consumed_at) VALUES ($1, $2, $3, $4)`, presented[:], actor.SessionID, familyID, now); err != nil {
		return identity.Session{}, fmt.Errorf("consume password-change refresh token: %w", err)
	}
	if _, err = tx.Exec(ctx, `UPDATE modura.auth_sessions SET refresh_token_hash = $2, security_version = $3, last_used_at = $4, expires_at = $5 WHERE id = $1`, actor.SessionID, next[:], securityVersion, now, expires); err != nil {
		return identity.Session{}, fmt.Errorf("rotate password-change session: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
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
	_, err = tx.Exec(ctx, `
UPDATE modura.auth_one_time_tokens
SET consumed_at = $4
WHERE tenant_id = $1 AND user_id = $2 AND purpose = $3 AND consumed_at IS NULL`, tenantID, userID, purpose, now)
	if err != nil {
		return fmt.Errorf("invalidate previous one-time tokens: %w", err)
	}
	const insert = `
INSERT INTO modura.auth_one_time_tokens
    (id, tenant_id, user_id, purpose, token_hash, created_at, expires_at)
SELECT $1, u.tenant_id, u.id, $4, $5, $6, $7
FROM modura.users u
JOIN modura.tenants t ON t.id = u.tenant_id
WHERE u.tenant_id = $2 AND u.id = $3 AND t.status = 'active'
  AND (($4 = 'invitation' AND u.status = 'invited') OR ($4 = 'password_reset' AND u.status = 'active'))`
	command, err := tx.Exec(ctx, insert, id, tenantID, userID, purpose, tokenHash[:], now, expires)
	if err != nil {
		return fmt.Errorf("insert one-time token: %w", err)
	}
	if command.RowsAffected() != 1 {
		return identity.ErrInactiveUser
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit one-time token creation: %w", err)
	}
	return nil
}

// ConsumeOneTimeToken atomically consumes a token, updates credentials, and
// invalidates every existing session for the user.
func (s *Store) ConsumeOneTimeToken(ctx context.Context, tokenHash [32]byte, purpose identity.OneTimePurpose, passwordHash string, now time.Time) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin one-time token consumption: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var tenantID identity.TenantID
	var userID identity.UserID
	var expiresAt time.Time
	const lock = `
SELECT tok.tenant_id, tok.user_id, tok.expires_at
FROM modura.auth_one_time_tokens tok
JOIN modura.users u ON u.tenant_id = tok.tenant_id AND u.id = tok.user_id
JOIN modura.tenants t ON t.id = tok.tenant_id
WHERE tok.token_hash = $1 AND tok.purpose = $2 AND tok.consumed_at IS NULL
  AND t.status = 'active'
  AND (($2 = 'invitation' AND u.status = 'invited') OR ($2 = 'password_reset' AND u.status = 'active'))
FOR UPDATE OF tok, u`
	err = tx.QueryRow(ctx, lock, tokenHash[:], purpose).Scan(&tenantID, &userID, &expiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return identity.ErrInvalidToken
	}
	if err != nil {
		return fmt.Errorf("lock one-time token: %w", err)
	}
	if !expiresAt.After(now) {
		return identity.ErrExpiredToken
	}
	var updateUser string
	if purpose == identity.PurposeInvitation {
		updateUser = `
UPDATE modura.users
SET password_hash = $3, security_version = security_version + 1, status = 'active',
    email_verified_at = CASE WHEN email IS NULL THEN NULL ELSE COALESCE(email_verified_at, $4) END,
    updated_at = $4
WHERE tenant_id = $1 AND id = $2`
	} else {
		updateUser = `
UPDATE modura.users
SET password_hash = $3, security_version = security_version + 1, updated_at = $4
WHERE tenant_id = $1 AND id = $2`
	}
	if _, err = tx.Exec(ctx, updateUser, tenantID, userID, passwordHash, now); err != nil {
		return fmt.Errorf("update one-time token credential: %w", err)
	}
	if _, err = tx.Exec(ctx, `UPDATE modura.auth_one_time_tokens SET consumed_at = $2 WHERE token_hash = $1`, tokenHash[:], now); err != nil {
		return fmt.Errorf("consume one-time token: %w", err)
	}
	if _, err = tx.Exec(ctx, `UPDATE modura.auth_sessions SET revoked_at = $3, revocation_reason = $4 WHERE tenant_id = $1 AND user_id = $2 AND revoked_at IS NULL`, tenantID, userID, now, string(purpose)); err != nil {
		return fmt.Errorf("revoke sessions after one-time token: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit one-time token consumption: %w", err)
	}
	return nil
}

// DisableAccount atomically disables a user, advances its security version,
// and revokes all sessions. Repeated disable requests are idempotent.
func (s *Store) DisableAccount(ctx context.Context, tenantID identity.TenantID, userID identity.UserID, reason string, now time.Time) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin account disable: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	command, err := tx.Exec(ctx, `
UPDATE modura.users
SET status = 'disabled', security_version = security_version + 1, updated_at = $3
WHERE tenant_id = $1 AND id = $2 AND status IN ('active', 'locked')`, tenantID, userID, now)
	if err != nil {
		return fmt.Errorf("update disabled account: %w", err)
	}
	if command.RowsAffected() == 0 {
		var status string
		if err := tx.QueryRow(ctx, `SELECT status FROM modura.users WHERE tenant_id = $1 AND id = $2`, tenantID, userID).Scan(&status); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return identity.ErrInactiveUser
			}
			return fmt.Errorf("check disabled account: %w", err)
		}
		if status != "disabled" {
			return identity.ErrInactiveUser
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE modura.auth_sessions SET revoked_at = $3, revocation_reason = $4 WHERE tenant_id = $1 AND user_id = $2 AND revoked_at IS NULL`, tenantID, userID, now, reason); err != nil {
		return fmt.Errorf("revoke disabled account sessions: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit account disable: %w", err)
	}
	return nil
}

// UnlockAccount restores only abuse-locked users; active retries are
// idempotent and administratively disabled users remain disabled.
func (s *Store) UnlockAccount(ctx context.Context, tenantID identity.TenantID, userID identity.UserID, now time.Time) error {
	command, err := s.pool.Exec(ctx, `UPDATE modura.users SET status = 'active', updated_at = $3 WHERE tenant_id = $1 AND id = $2 AND status = 'locked'`, tenantID, userID, now)
	if err != nil {
		return fmt.Errorf("unlock account: %w", err)
	}
	if command.RowsAffected() == 1 {
		return nil
	}
	var status string
	if err := s.pool.QueryRow(ctx, `SELECT status FROM modura.users WHERE tenant_id = $1 AND id = $2`, tenantID, userID).Scan(&status); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return identity.ErrInactiveUser
		}
		return fmt.Errorf("check unlocked account: %w", err)
	}
	if status == "active" {
		return nil
	}
	return identity.ErrInactiveUser
}

// ProvisionTenant creates identity-owned provisioning records in a workflow transaction.
func (s *Store) ProvisionTenant(ctx context.Context, tx pgx.Tx, provisioning identity.TenantProvisioning) error {
	if _, err := tx.Exec(ctx, `
INSERT INTO modura.tenants (id, slug, display_name, status, created_at, updated_at)
VALUES ($1, $2, $3, 'provisioning', $4, $4)`, provisioning.TenantID, provisioning.Slug, provisioning.DisplayName, provisioning.CreatedAt); err != nil {
		return fmt.Errorf("insert provisioning tenant: %w", err)
	}
	var email any
	var normalizedEmail any
	if provisioning.Email != "" {
		email, normalizedEmail = provisioning.Email, provisioning.NormalizedEmail
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO modura.users
    (id, tenant_id, username, normalized_username, email, normalized_email, status, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, 'invited', $7, $7)`, provisioning.AdministratorID, provisioning.TenantID, provisioning.Username, provisioning.NormalizedUsername, email, normalizedEmail, provisioning.CreatedAt); err != nil {
		return fmt.Errorf("insert invited administrator: %w", err)
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO modura.auth_one_time_tokens
    (id, tenant_id, user_id, purpose, token_hash, created_at, expires_at)
VALUES ($1, $2, $3, 'invitation', $4, $5, $6)`, provisioning.InvitationID, provisioning.TenantID, provisioning.AdministratorID, provisioning.InvitationHash[:], provisioning.CreatedAt, provisioning.InvitationExpires); err != nil {
		return fmt.Errorf("insert administrator invitation: %w", err)
	}
	return nil
}

// ActivateTenant transitions only a provisioning tenant to active.
func (s *Store) ActivateTenant(ctx context.Context, tx pgx.Tx, tenantID identity.TenantID, now time.Time) error {
	command, err := tx.Exec(ctx, `UPDATE modura.tenants SET status = 'active', updated_at = $2 WHERE id = $1 AND status = 'provisioning'`, tenantID, now)
	if err != nil {
		return fmt.Errorf("update tenant active: %w", err)
	}
	if command.RowsAffected() != 1 {
		return identity.ErrInactiveTenant
	}
	return nil
}

func (s *Store) revoke(ctx context.Context, query string, tenantID identity.TenantID, userID identity.UserID, sessionID identity.SessionID, now time.Time, reason string) error {
	_, err := s.pool.Exec(ctx, query, tenantID, userID, sessionID, now, reason)
	if err != nil {
		return fmt.Errorf("revoke session: %w", err)
	}
	return nil
}
