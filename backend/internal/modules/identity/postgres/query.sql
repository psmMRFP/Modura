-- name: TenantUserByID :one
SELECT id, username, email, status, created_at, updated_at
FROM modura.users
WHERE tenant_id = $1 AND id = $2;

-- name: TenantUserStatusByID :one
SELECT status
FROM modura.users
WHERE tenant_id = $1 AND id = $2;

-- name: DisableTenantUser :execrows
UPDATE modura.users
SET status = 'disabled', security_version = security_version + 1, updated_at = $3
WHERE tenant_id = $1 AND id = $2 AND status <> 'disabled';

-- name: UnlockTenantUser :execrows
UPDATE modura.users
SET status = 'active', security_version = security_version + 1, updated_at = $3
WHERE tenant_id = $1 AND id = $2 AND status = 'locked';

-- name: RevokeTenantUserSessions :exec
UPDATE modura.auth_sessions
SET revoked_at = $3, revocation_reason = $4
WHERE tenant_id = $1 AND user_id = $2 AND revoked_at IS NULL;

-- name: ConsumeTenantUserOneTimeTokens :exec
UPDATE modura.auth_one_time_tokens
SET consumed_at = $3
WHERE tenant_id = $1 AND user_id = $2 AND consumed_at IS NULL;

-- name: LockSelfProfile :one
SELECT u.id, u.username, u.email, u.status, u.updated_at
FROM modura.users u
JOIN modura.auth_sessions s ON s.tenant_id = u.tenant_id AND s.user_id = u.id
WHERE u.tenant_id = $1 AND u.id = $2 AND s.id = $3 AND u.status = 'active' AND s.revoked_at IS NULL
FOR UPDATE OF u;

-- name: UpdateSelfProfile :exec
UPDATE modura.users
SET username = $3, normalized_username = $4, email = $5, normalized_email = $6,
    email_verified_at = CASE WHEN normalized_email IS NOT DISTINCT FROM $6 THEN email_verified_at ELSE NULL END,
    updated_at = $7
WHERE tenant_id = $1 AND id = $2;

-- name: ListTenantSummaries :many
SELECT id, slug, display_name, status, created_at, updated_at
FROM modura.tenants
ORDER BY created_at, id;

-- name: LockTenantProfile :one
SELECT slug, display_name, status, updated_at
FROM modura.tenants
WHERE id = $1
FOR UPDATE;

-- name: UpdateTenantProfile :exec
UPDATE modura.tenants
SET display_name = $2, updated_at = $3
WHERE id = $1;

-- name: TransitionTenantStatus :execrows
UPDATE modura.tenants
SET status = $3, updated_at = $4
WHERE id = $1 AND status = $2;

-- name: TenantStatusByID :one
SELECT status
FROM modura.tenants
WHERE id = $1;

-- name: UserExistsInTenant :one
SELECT EXISTS (SELECT 1 FROM modura.users WHERE tenant_id = $1 AND id = $2) AS present;

-- name: LoginGuardLockedUntil :one
SELECT locked_until
FROM modura.auth_login_guard
WHERE tenant_slug = $1 AND normalized_login = $2;

-- name: RecordLoginFailure :one
INSERT INTO modura.auth_login_guard
    (tenant_slug, normalized_login, failure_count, window_started_at, locked_until, updated_at)
VALUES ($1, $2, 1, @now::timestamptz, NULL, @now::timestamptz)
ON CONFLICT (tenant_slug, normalized_login) DO UPDATE
SET failure_count = CASE
        WHEN modura.auth_login_guard.window_started_at >= @cutoff::timestamptz THEN modura.auth_login_guard.failure_count + 1
        ELSE 1 END,
    window_started_at = CASE
        WHEN modura.auth_login_guard.window_started_at >= @cutoff::timestamptz THEN modura.auth_login_guard.window_started_at
        ELSE @now::timestamptz END,
    locked_until = CASE
        WHEN (
            CASE WHEN modura.auth_login_guard.window_started_at >= @cutoff::timestamptz THEN modura.auth_login_guard.failure_count + 1
            ELSE 1 END
        ) >= @threshold::int THEN @lock_until::timestamptz
        ELSE NULL END,
    updated_at = @now::timestamptz
RETURNING failure_count, locked_until;

-- name: ClearLoginGuard :execrows
DELETE FROM modura.auth_login_guard
WHERE tenant_slug = $1 AND normalized_login = $2;

-- name: InsertAuthSecurityEvent :exec
INSERT INTO modura.auth_security_events (id, tenant_id, user_id, event_type, correlation_id, occurred_at)
VALUES ($1, $2, $3, $4, $5, $6);

-- name: ProfileBySession :one
SELECT u.id, u.username, u.email, u.status, u.updated_at
FROM modura.users u
JOIN modura.auth_sessions s ON s.tenant_id = u.tenant_id AND s.user_id = u.id
WHERE u.tenant_id = $1 AND u.id = $2 AND s.id = $3 AND u.status = 'active' AND s.revoked_at IS NULL;

-- name: UpdatePasswordHash :execrows
UPDATE modura.users
SET password_hash = $3, updated_at = now()
WHERE tenant_id = $1 AND id = $2 AND status = 'active';

-- name: InsertAuthSession :exec
INSERT INTO modura.auth_sessions
    (id, tenant_id, user_id, family_id, refresh_token_hash, security_version, created_at, last_used_at, expires_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $7, $8);

-- name: ReplayedTokenFamily :one
SELECT r.family_id FROM modura.auth_refresh_token_uses r
JOIN modura.auth_sessions s ON s.id=r.session_id
JOIN modura.users u ON u.id=s.user_id AND u.tenant_id=s.tenant_id
WHERE r.token_hash=$1 AND u.consumer=sqlc.arg(consumer)::boolean;

-- name: SessionFamilyOwner :one
SELECT tenant_id, user_id
FROM modura.auth_sessions
WHERE family_id = $1
LIMIT 1;

-- name: RevokeFamilySessions :execrows
UPDATE modura.auth_sessions
SET revoked_at = $2, revocation_reason = 'refresh_reuse'
WHERE family_id = $1 AND revoked_at IS NULL;

-- name: LockCurrentSession :one
SELECT s.id, s.tenant_id, s.user_id, s.security_version, s.family_id, s.expires_at
FROM modura.auth_sessions s
JOIN modura.users u ON u.tenant_id = s.tenant_id AND u.id = s.user_id
JOIN modura.tenants t ON t.id = s.tenant_id
WHERE s.refresh_token_hash = $1 AND s.revoked_at IS NULL
  AND u.status = 'active' AND u.security_version = s.security_version AND t.status = 'active'
  AND u.consumer=sqlc.arg(consumer)::boolean
FOR UPDATE OF s;

-- name: RecordRefreshTokenUse :exec
INSERT INTO modura.auth_refresh_token_uses (token_hash, session_id, family_id, consumed_at)
VALUES ($1, $2, $3, $4);

-- name: RotateSessionSecret :exec
UPDATE modura.auth_sessions
SET refresh_token_hash = $2, last_used_at = $3, expires_at = $4
WHERE id = $1;

-- name: RevokeUserSession :execrows
UPDATE modura.auth_sessions
SET revoked_at = $4, revocation_reason = $5
WHERE tenant_id = $1 AND user_id = $2 AND id = $3 AND revoked_at IS NULL;

-- name: RevokeSessionsExcept :exec
UPDATE modura.auth_sessions
SET revoked_at = $4, revocation_reason = $5
WHERE tenant_id = $1 AND user_id = $2 AND id <> $3 AND revoked_at IS NULL;

-- name: RevokeAllUserSessions :exec
UPDATE modura.auth_sessions
SET revoked_at = $3, revocation_reason = $4
WHERE tenant_id = $1 AND user_id = $2 AND revoked_at IS NULL;

-- name: SessionSecurityActive :one
SELECT 1
FROM modura.auth_sessions s
JOIN modura.users u ON u.tenant_id = s.tenant_id AND u.id = s.user_id
JOIN modura.tenants t ON t.id = s.tenant_id
WHERE s.id = $1 AND s.tenant_id = $2 AND s.user_id = $3
  AND s.security_version = $4 AND u.security_version = $4
  AND s.revoked_at IS NULL AND s.expires_at > $5
  AND u.status = 'active' AND t.status = 'active' AND u.consumer=sqlc.arg(consumer)::boolean;

-- name: PasswordHashBySession :one
SELECT u.password_hash
FROM modura.users u
JOIN modura.auth_sessions s ON s.tenant_id = u.tenant_id AND s.user_id = u.id
JOIN modura.tenants t ON t.id = u.tenant_id
WHERE u.tenant_id = $1 AND u.id = $2 AND s.id = $3
  AND u.status = 'active' AND t.status = 'active' AND s.revoked_at IS NULL;

-- name: LockPasswordChange :one
SELECT s.family_id, u.security_version
FROM modura.auth_sessions s
JOIN modura.users u ON u.tenant_id = s.tenant_id AND u.id = s.user_id
JOIN modura.tenants t ON t.id = s.tenant_id
WHERE s.id = $1 AND s.tenant_id = $2 AND s.user_id = $3
  AND s.refresh_token_hash = $4 AND s.revoked_at IS NULL AND s.expires_at > $5
  AND u.password_hash = $6 AND u.status = 'active' AND t.status = 'active'
FOR UPDATE OF s, u;

-- name: ApplyPasswordChange :execrows
UPDATE modura.users
SET password_hash = $3, security_version = $4, updated_at = $5
WHERE tenant_id = $1 AND id = $2;

-- name: RotateSessionAfterPasswordChange :exec
UPDATE modura.auth_sessions
SET refresh_token_hash = $2, security_version = $3, last_used_at = $4, expires_at = $5
WHERE id = $1;

-- name: InvalidateOneTimeTokens :exec
UPDATE modura.auth_one_time_tokens
SET consumed_at = $4
WHERE tenant_id = $1 AND user_id = $2 AND purpose = $3 AND consumed_at IS NULL;

-- name: InsertOneTimeToken :execrows
INSERT INTO modura.auth_one_time_tokens
    (id, tenant_id, user_id, purpose, token_hash, created_at, expires_at)
SELECT $1, u.tenant_id, u.id, $4, $5, $6, $7
FROM modura.users u
JOIN modura.tenants t ON t.id = u.tenant_id
WHERE u.tenant_id = $2 AND u.id = $3 AND t.status = 'active'
  AND (($4 = 'invitation' AND u.status = 'invited') OR ($4 = 'password_reset' AND u.status = 'active'));

-- name: LockOneTimeToken :one
SELECT tok.tenant_id, tok.user_id, tok.expires_at
FROM modura.auth_one_time_tokens tok
JOIN modura.users u ON u.tenant_id = tok.tenant_id AND u.id = tok.user_id
JOIN modura.tenants t ON t.id = tok.tenant_id
WHERE tok.token_hash = $1 AND tok.purpose = $2 AND tok.consumed_at IS NULL AND NOT u.consumer
  AND t.status = 'active'
  AND (($2 = 'invitation' AND u.status = 'invited') OR ($2 = 'password_reset' AND u.status = 'active'))
FOR UPDATE OF tok, u;

-- name: ApplyInvitationActivation :exec
UPDATE modura.users
SET password_hash = $3, security_version = security_version + 1, status = 'active',
    email_verified_at = CASE WHEN email IS NULL THEN NULL ELSE COALESCE(email_verified_at, $4) END,
    updated_at = $4
WHERE tenant_id = $1 AND id = $2;

-- name: ApplyPasswordReset :exec
UPDATE modura.users
SET password_hash = $3, security_version = security_version + 1, updated_at = $4
WHERE tenant_id = $1 AND id = $2;

-- name: ConsumeOneTimeTokenRow :execrows
UPDATE modura.auth_one_time_tokens
SET consumed_at = $2
WHERE token_hash = $1;

-- name: InsertProvisioningTenant :exec
INSERT INTO modura.tenants (id, slug, display_name, status, created_at, updated_at)
VALUES ($1, $2, $3, 'provisioning', $4, $4);

-- name: InsertInvitedAdministrator :exec
INSERT INTO modura.users
    (id, tenant_id, username, normalized_username, email, normalized_email, status, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, 'invited', $7, $7);

-- name: InsertAdministratorInvitation :exec
INSERT INTO modura.auth_one_time_tokens
    (id, tenant_id, user_id, purpose, token_hash, created_at, expires_at)
VALUES ($1, $2, $3, 'invitation', $4, $5, $6);

-- name: ActivateProvisioningTenant :execrows
UPDATE modura.tenants
SET status = 'active', updated_at = $2
WHERE id = $1 AND status = 'provisioning';

-- name: LockCommunityBootstrap :exec
SELECT pg_advisory_xact_lock(1297040471);

-- name: CommunityBinding :one
SELECT t.id, t.slug, t.status
FROM modura.community_identity c JOIN modura.tenants t ON t.id = c.tenant_id
WHERE c.singleton;

-- name: InsertCommunityTenant :exec
INSERT INTO modura.tenants (id, slug, display_name, status, created_at, updated_at)
VALUES ($1, 'community', 'WhereToLive Community', 'active', $2, $2);

-- name: InsertCommunityBinding :exec
INSERT INTO modura.community_identity (tenant_id, created_at) VALUES ($1, $2);

-- name: InsertPublicIdentityEvent :exec
INSERT INTO modura.public_identity_events (id, tenant_id, user_id, action, result, correlation_id, occurred_at, actor_kind, resource, resource_id, reason)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11);

-- name: LockPublicIdentityTenant :one
SELECT t.id FROM modura.community_identity c JOIN modura.tenants t ON t.id = c.tenant_id
WHERE c.tenant_id = $1 AND t.slug = 'community' AND t.status = 'active' FOR SHARE OF t;

-- name: InsertConsumer :execrows
INSERT INTO modura.users (id, tenant_id, username, normalized_username, email, normalized_email,
    password_hash, status, consumer, created_at, updated_at)
VALUES ($1, $2, $3, $3, $4, $4, $5, 'pending_email', true, $6, $6)
ON CONFLICT DO NOTHING;

-- name: ConsumerByEmail :one
SELECT id, normalized_email, security_version, status FROM modura.users
WHERE tenant_id = $1 AND normalized_email = $2 AND consumer FOR UPDATE;

-- name: InsertPublicIdentityToken :exec
INSERT INTO modura.auth_one_time_tokens (id, tenant_id, user_id, purpose, token_hash, created_at, expires_at, bound_email, bound_security_version)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9);

-- name: InsertIdentityMail :exec
INSERT INTO modura.identity_mail_queue (id, tenant_id, user_id, token_id, encrypted_payload, created_at, expires_at, available_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $6);

-- name: CancelIdentityMail :exec
DELETE FROM modura.identity_mail_queue WHERE tenant_id=$1 AND user_id=$2;

-- name: LockPublicIdentityToken :one
SELECT tok.id, tok.user_id, tok.expires_at, u.status
FROM modura.auth_one_time_tokens tok JOIN modura.users u ON u.tenant_id=tok.tenant_id AND u.id=tok.user_id
WHERE tok.tenant_id=$1 AND tok.token_hash=$2 AND tok.purpose=$3 AND tok.consumed_at IS NULL AND u.consumer
 AND tok.bound_email=u.normalized_email AND tok.bound_security_version=u.security_version
FOR UPDATE OF u, tok;

-- name: VerifyConsumerEmail :exec
UPDATE modura.users SET email_verified_at=$3, status='active', security_version=security_version+1, updated_at=$3
WHERE tenant_id=$1 AND id=$2 AND status='pending_email' AND consumer;

-- name: ConsumePublicIdentityLimit :one
INSERT INTO modura.public_identity_limits (tenant_id,key_hash,window_started_at,attempts)
VALUES ($1,$2,@now::timestamptz,1)
ON CONFLICT (tenant_id,key_hash) DO UPDATE SET
    window_started_at = CASE WHEN modura.public_identity_limits.window_started_at <= @cutoff::timestamptz THEN @now::timestamptz ELSE modura.public_identity_limits.window_started_at END,
    attempts = CASE WHEN modura.public_identity_limits.window_started_at <= @cutoff::timestamptz THEN 1 ELSE modura.public_identity_limits.attempts+1 END
RETURNING attempts;

-- name: PurgePublicIdentityLimits :exec
DELETE FROM modura.public_identity_limits WHERE window_started_at < $1;

-- name: PurgeIdentityMail :exec
DELETE FROM modura.identity_mail_queue WHERE expires_at <= $1 OR attempts >= 5;

-- name: LeaseIdentityMail :one
WITH next AS (
 SELECT q.id FROM modura.identity_mail_queue q
 JOIN modura.auth_one_time_tokens tok ON tok.id=q.token_id
 JOIN modura.users u ON u.tenant_id=q.tenant_id AND u.id=q.user_id
 JOIN modura.tenants t ON t.id=q.tenant_id
 WHERE q.available_at <= @now::timestamptz AND q.expires_at > @now::timestamptz
 AND (q.lease_until IS NULL OR q.lease_until <= @now::timestamptz)
 AND tok.consumed_at IS NULL AND t.status='active' AND u.consumer
 AND ((tok.purpose='email_verification' AND u.status='pending_email') OR (tok.purpose='password_reset' AND u.status='active'))
 ORDER BY q.created_at FOR UPDATE OF q SKIP LOCKED LIMIT 1
)
UPDATE modura.identity_mail_queue q SET lease_until= @until::timestamptz, attempts=attempts+1
FROM next WHERE q.id=next.id RETURNING q.id, q.tenant_id, q.encrypted_payload, q.attempts;

-- name: FinishIdentityMail :exec
DELETE FROM modura.identity_mail_queue WHERE id=$1 AND tenant_id=$2 AND attempts=$3;

-- name: RetryIdentityMail :exec
UPDATE modura.identity_mail_queue SET available_at=$4, lease_until=NULL WHERE id=$1 AND tenant_id=$2 AND attempts=$3;

-- name: PublicIdentitySchemaExists :one
SELECT (to_regclass('modura.identity_mail_queue') IS NOT NULL)::boolean AS available;

-- name: PurgeExpiredConsumerTokens :exec
DELETE FROM modura.auth_one_time_tokens tok WHERE tok.bound_email IS NOT NULL AND tok.expires_at < $1
AND NOT EXISTS (SELECT 1 FROM modura.identity_mail_queue q WHERE q.token_id=tok.id);
