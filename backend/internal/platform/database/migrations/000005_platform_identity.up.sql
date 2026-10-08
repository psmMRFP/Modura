-- owner: platformadmin
CREATE TABLE wheretolive.platform_administrators (
    id uuid PRIMARY KEY,
    username text NOT NULL,
    normalized_username text NOT NULL UNIQUE,
    password_hash text NOT NULL,
    status text NOT NULL CHECK (status IN ('active', 'disabled', 'locked')),
    security_version bigint NOT NULL DEFAULT 1 CHECK (security_version > 0),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CONSTRAINT platform_administrators_username_present CHECK (btrim(username) <> ''),
    CONSTRAINT platform_administrators_username_normalized CHECK (normalized_username = lower(btrim(normalized_username)))
);

CREATE TABLE wheretolive.platform_auth_sessions (
    id uuid PRIMARY KEY,
    administrator_id uuid NOT NULL REFERENCES wheretolive.platform_administrators (id),
    family_id uuid NOT NULL,
    refresh_token_hash bytea NOT NULL UNIQUE,
    security_version bigint NOT NULL CHECK (security_version > 0),
    created_at timestamptz NOT NULL,
    last_used_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL,
    revoked_at timestamptz,
    revocation_reason text,
    CONSTRAINT platform_auth_sessions_expiry CHECK (expires_at > created_at),
    CONSTRAINT platform_auth_sessions_revocation_pair CHECK ((revoked_at IS NULL) = (revocation_reason IS NULL))
);

CREATE INDEX platform_auth_sessions_administrator_active_idx
    ON wheretolive.platform_auth_sessions (administrator_id) WHERE revoked_at IS NULL;
CREATE INDEX platform_auth_sessions_family_idx ON wheretolive.platform_auth_sessions (family_id);

CREATE TABLE wheretolive.platform_refresh_token_uses (
    token_hash bytea PRIMARY KEY,
    session_id uuid NOT NULL REFERENCES wheretolive.platform_auth_sessions (id),
    family_id uuid NOT NULL,
    consumed_at timestamptz NOT NULL
);

CREATE INDEX platform_refresh_token_uses_family_idx
    ON wheretolive.platform_refresh_token_uses (family_id);
