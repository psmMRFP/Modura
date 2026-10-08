-- owner: identity
CREATE TABLE wheretolive.tenants (
    id uuid PRIMARY KEY,
    slug text NOT NULL,
    display_name text NOT NULL,
    status text NOT NULL CHECK (status IN ('provisioning', 'active', 'suspended')),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CONSTRAINT tenants_slug_normalized CHECK (slug = lower(btrim(slug))),
    CONSTRAINT tenants_slug_unique UNIQUE (slug)
);

CREATE TABLE wheretolive.users (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL REFERENCES wheretolive.tenants (id),
    username text NOT NULL,
    normalized_username text NOT NULL,
    email text,
    normalized_email text,
    email_verified_at timestamptz,
    password_hash text,
    status text NOT NULL CHECK (status IN ('invited', 'active', 'disabled', 'locked')),
    security_version bigint NOT NULL DEFAULT 1 CHECK (security_version > 0),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CONSTRAINT users_tenant_id_id_unique UNIQUE (tenant_id, id),
    CONSTRAINT users_username_unique UNIQUE (tenant_id, normalized_username),
    CONSTRAINT users_email_unique UNIQUE (tenant_id, normalized_email),
    CONSTRAINT users_email_pair CHECK ((email IS NULL) = (normalized_email IS NULL)),
    CONSTRAINT users_verified_email CHECK (email_verified_at IS NULL OR normalized_email IS NOT NULL),
    CONSTRAINT users_active_credential CHECK (status <> 'active' OR password_hash IS NOT NULL)
);

CREATE TABLE wheretolive.auth_sessions (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL,
    user_id uuid NOT NULL,
    family_id uuid NOT NULL,
    refresh_token_hash bytea NOT NULL UNIQUE,
    security_version bigint NOT NULL CHECK (security_version > 0),
    created_at timestamptz NOT NULL,
    last_used_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL,
    revoked_at timestamptz,
    revocation_reason text,
    CONSTRAINT auth_sessions_user_fk FOREIGN KEY (tenant_id, user_id)
        REFERENCES wheretolive.users (tenant_id, id),
    CONSTRAINT auth_sessions_expiry CHECK (expires_at > created_at),
    CONSTRAINT auth_sessions_revocation_pair CHECK ((revoked_at IS NULL) = (revocation_reason IS NULL))
);

CREATE INDEX auth_sessions_user_active_idx ON wheretolive.auth_sessions (tenant_id, user_id)
    WHERE revoked_at IS NULL;
CREATE INDEX auth_sessions_family_idx ON wheretolive.auth_sessions (family_id);

CREATE TABLE wheretolive.auth_refresh_token_uses (
    token_hash bytea PRIMARY KEY,
    session_id uuid NOT NULL REFERENCES wheretolive.auth_sessions (id),
    family_id uuid NOT NULL,
    consumed_at timestamptz NOT NULL
);

CREATE INDEX auth_refresh_token_uses_family_idx ON wheretolive.auth_refresh_token_uses (family_id);

CREATE TABLE wheretolive.auth_one_time_tokens (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL,
    user_id uuid NOT NULL,
    purpose text NOT NULL CHECK (purpose IN ('invitation', 'password_reset')),
    token_hash bytea NOT NULL UNIQUE,
    created_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL,
    consumed_at timestamptz,
    CONSTRAINT auth_one_time_tokens_user_fk FOREIGN KEY (tenant_id, user_id)
        REFERENCES wheretolive.users (tenant_id, id),
    CONSTRAINT auth_one_time_tokens_expiry CHECK (expires_at > created_at)
);

CREATE INDEX auth_one_time_tokens_lookup_idx ON wheretolive.auth_one_time_tokens (tenant_id, user_id, purpose)
    WHERE consumed_at IS NULL;
