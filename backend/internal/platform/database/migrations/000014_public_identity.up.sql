-- owner: identity
ALTER TABLE wheretolive.users DROP CONSTRAINT users_status_check;
ALTER TABLE wheretolive.users ADD CONSTRAINT users_status_check
    CHECK (status IN ('invited', 'pending_email', 'active', 'disabled', 'locked'));
ALTER TABLE wheretolive.users ADD COLUMN consumer boolean NOT NULL DEFAULT false;
ALTER TABLE wheretolive.users ADD CONSTRAINT consumer_verified_active CHECK
    (NOT consumer OR (email IS NOT NULL AND password_hash IS NOT NULL
        AND (status <> 'active' OR email_verified_at IS NOT NULL)));
ALTER TABLE wheretolive.auth_one_time_tokens DROP CONSTRAINT auth_one_time_tokens_purpose_check;
ALTER TABLE wheretolive.auth_one_time_tokens ADD CONSTRAINT auth_one_time_tokens_purpose_check
    CHECK (purpose IN ('invitation', 'password_reset', 'email_verification'));

ALTER TABLE wheretolive.auth_one_time_tokens ADD CONSTRAINT auth_one_time_tokens_scope_unique UNIQUE(tenant_id,user_id,id);
ALTER TABLE wheretolive.auth_one_time_tokens ADD COLUMN bound_email text;
ALTER TABLE wheretolive.auth_one_time_tokens ADD COLUMN bound_security_version bigint;

CREATE TABLE wheretolive.community_identity (
    singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
    tenant_id uuid NOT NULL UNIQUE REFERENCES wheretolive.tenants(id),
    created_at timestamptz NOT NULL
);

CREATE TABLE wheretolive.public_identity_events (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL REFERENCES wheretolive.tenants(id),
    user_id uuid,
    actor_kind text NOT NULL CHECK (actor_kind IN ('system', 'anonymous_consumer')),
    resource text NOT NULL,
    resource_id uuid NOT NULL,
    reason text NOT NULL CHECK (btrim(reason) <> ''),
    action text NOT NULL,
    result text NOT NULL CHECK (result IN ('success', 'failure')),
    correlation_id text NOT NULL CHECK (btrim(correlation_id) <> ''),
    occurred_at timestamptz NOT NULL,
    FOREIGN KEY (tenant_id, user_id) REFERENCES wheretolive.users(tenant_id, id)
);

CREATE TABLE wheretolive.identity_mail_queue (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL,
    user_id uuid NOT NULL,
    token_id uuid NOT NULL,
    encrypted_payload bytea NOT NULL,
    created_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL CHECK (expires_at > created_at),
    available_at timestamptz NOT NULL,
    lease_until timestamptz,
    attempts integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    FOREIGN KEY (tenant_id,user_id,token_id) REFERENCES wheretolive.auth_one_time_tokens(tenant_id,user_id,id),
    FOREIGN KEY (tenant_id, user_id) REFERENCES wheretolive.users(tenant_id, id)
);
CREATE INDEX identity_mail_queue_available_idx ON wheretolive.identity_mail_queue(available_at);

CREATE TABLE wheretolive.public_identity_limits (
    tenant_id uuid NOT NULL REFERENCES wheretolive.tenants(id),
    key_hash bytea NOT NULL CHECK (octet_length(key_hash) = 32),
    window_started_at timestamptz NOT NULL,
    attempts integer NOT NULL CHECK (attempts > 0),
    PRIMARY KEY (tenant_id, key_hash)
);
