// Package postgres persists global platform-administrator identity data.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/psmMRFP/WhereToLive/backend/internal/modules/platformadmin"
)

// Store persists platform administrators and their sessions.
type Store struct{ pool *pgxpool.Pool }

// New constructs a PostgreSQL platform-administrator store.
func New(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

// Bootstrap atomically creates the first and only bootstrap administrator.
func (s *Store) Bootstrap(ctx context.Context, id platformadmin.AdministratorID, username, normalized, passwordHash string, now time.Time) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin platform bootstrap: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(1297040470)"); err != nil {
		return fmt.Errorf("lock platform bootstrap: %w", err)
	}
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM wheretolive.platform_administrators)`).Scan(&exists); err != nil {
		return fmt.Errorf("check platform bootstrap: %w", err)
	}
	if exists {
		return platformadmin.ErrBootstrapComplete
	}
	if _, err := tx.Exec(ctx, `INSERT INTO wheretolive.platform_administrators (id, username, normalized_username, password_hash, status, created_at, updated_at) VALUES ($1, $2, $3, $4, 'active', $5, $5)`, id, username, normalized, passwordHash, now); err != nil {
		return fmt.Errorf("insert platform administrator: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit platform bootstrap: %w", err)
	}
	return nil
}

// FindActiveAccount finds a global administrator without tenant context.
func (s *Store) FindActiveAccount(ctx context.Context, normalized string) (platformadmin.Account, error) {
	var account platformadmin.Account
	err := s.pool.QueryRow(ctx, `SELECT id, password_hash, security_version FROM wheretolive.platform_administrators WHERE normalized_username = $1 AND status = 'active'`, normalized).Scan(&account.ID, &account.PasswordHash, &account.SecurityVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return platformadmin.Account{}, platformadmin.ErrInvalidCredentials
	}
	if err != nil {
		return platformadmin.Account{}, fmt.Errorf("find platform administrator: %w", err)
	}
	return account, nil
}

// CreateSession persists a new platform refresh family.
func (s *Store) CreateSession(ctx context.Context, session platformadmin.NewSession) error {
	_, err := s.pool.Exec(ctx, `
INSERT INTO wheretolive.platform_auth_sessions
    (id, administrator_id, family_id, refresh_token_hash, security_version, created_at, last_used_at, expires_at)
VALUES ($1, $2, $3, $4, $5, $6, $6, $7)`, session.ID, session.AdministratorID, session.FamilyID, session.RefreshHash[:], session.SecurityVersion, session.CreatedAt, session.ExpiresAt)
	if err != nil {
		return fmt.Errorf("create platform session: %w", err)
	}
	return nil
}

// RotateSession atomically rotates a platform refresh token and revokes replayed families.
func (s *Store) RotateSession(ctx context.Context, presented, next [32]byte, now, expires time.Time) (platformadmin.Session, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return platformadmin.Session{}, fmt.Errorf("begin platform refresh: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var replayFamily string
	err = tx.QueryRow(ctx, `SELECT family_id FROM wheretolive.platform_refresh_token_uses WHERE token_hash = $1`, presented[:]).Scan(&replayFamily)
	if err == nil {
		if _, err := tx.Exec(ctx, `UPDATE wheretolive.platform_auth_sessions SET revoked_at = $2, revocation_reason = 'refresh_reuse' WHERE family_id = $1 AND revoked_at IS NULL`, replayFamily, now); err != nil {
			return platformadmin.Session{}, fmt.Errorf("revoke platform replay family: %w", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return platformadmin.Session{}, fmt.Errorf("commit platform replay: %w", err)
		}
		return platformadmin.Session{}, platformadmin.ErrInvalidToken
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return platformadmin.Session{}, fmt.Errorf("check platform replay: %w", err)
	}
	var session platformadmin.Session
	var familyID string
	var expiresAt time.Time
	const query = `
SELECT s.id, s.administrator_id, s.security_version, s.family_id, s.expires_at
FROM wheretolive.platform_auth_sessions s
JOIN wheretolive.platform_administrators a ON a.id = s.administrator_id
WHERE s.refresh_token_hash = $1 AND s.revoked_at IS NULL
  AND a.status = 'active' AND a.security_version = s.security_version
FOR UPDATE OF s`
	err = tx.QueryRow(ctx, query, presented[:]).Scan(&session.ID, &session.AdministratorID, &session.SecurityVersion, &familyID, &expiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return platformadmin.Session{}, platformadmin.ErrInvalidToken
	}
	if err != nil {
		return platformadmin.Session{}, fmt.Errorf("lock platform session: %w", err)
	}
	if !expiresAt.After(now) {
		return platformadmin.Session{}, platformadmin.ErrInvalidToken
	}
	if _, err := tx.Exec(ctx, `INSERT INTO wheretolive.platform_refresh_token_uses (token_hash, session_id, family_id, consumed_at) VALUES ($1, $2, $3, $4)`, presented[:], session.ID, familyID, now); err != nil {
		return platformadmin.Session{}, fmt.Errorf("consume platform refresh token: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE wheretolive.platform_auth_sessions SET refresh_token_hash = $2, last_used_at = $3, expires_at = $4 WHERE id = $1`, session.ID, next[:], now, expires); err != nil {
		return platformadmin.Session{}, fmt.Errorf("rotate platform refresh token: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return platformadmin.Session{}, fmt.Errorf("commit platform refresh: %w", err)
	}
	return session, nil
}

// ValidateSession checks current global administrator and session state.
func (s *Store) ValidateSession(ctx context.Context, session platformadmin.Session, now time.Time) error {
	var exists int
	const query = `
SELECT 1 FROM wheretolive.platform_auth_sessions s
JOIN wheretolive.platform_administrators a ON a.id = s.administrator_id
WHERE s.id = $1 AND s.administrator_id = $2 AND s.security_version = $3
  AND a.security_version = $3 AND a.status = 'active'
  AND s.revoked_at IS NULL AND s.expires_at > $4`
	if err := s.pool.QueryRow(ctx, query, session.ID, session.AdministratorID, session.SecurityVersion, now).Scan(&exists); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return platformadmin.ErrInvalidToken
		}
		return fmt.Errorf("validate platform session: %w", err)
	}
	return nil
}

// RevokeSession revokes exactly the verified platform session.
func (s *Store) RevokeSession(ctx context.Context, actor platformadmin.Actor, reason string, now time.Time) error {
	command, err := s.pool.Exec(ctx, `UPDATE wheretolive.platform_auth_sessions SET revoked_at = $3, revocation_reason = $4 WHERE administrator_id = $1 AND id = $2 AND revoked_at IS NULL`, actor.AdministratorID, actor.SessionID, now, reason)
	if err != nil {
		return fmt.Errorf("revoke platform session: %w", err)
	}
	if command.RowsAffected() != 1 {
		return platformadmin.ErrInvalidToken
	}
	return nil
}
