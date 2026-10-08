-- owner: identity
-- Credential-throttling state is keyed by the submitted tenant slug and login
-- only. It deliberately stores no account reference so unknown logins are
-- indistinguishable from known ones in throttling behavior.
CREATE TABLE modura.auth_login_guard (
    tenant_slug text NOT NULL,
    normalized_login text NOT NULL,
    failure_count integer NOT NULL DEFAULT 0,
    window_started_at timestamptz NOT NULL,
    locked_until timestamptz,
    updated_at timestamptz NOT NULL,
    PRIMARY KEY (tenant_slug, normalized_login),
    CONSTRAINT auth_login_guard_failures_check CHECK (failure_count >= 0)
);

-- Privacy-conscious authentication security evidence: no credentials, login
-- strings, network addresses, or user agents are ever recorded.
CREATE TABLE modura.auth_security_events (
    id uuid PRIMARY KEY,
    tenant_id uuid REFERENCES modura.tenants (id),
    user_id uuid REFERENCES modura.users (id),
    event_type text NOT NULL CHECK (event_type IN (
        'login_failed', 'login_locked', 'refresh_replay_detected',
        'password_changed', 'sessions_revoked'
    )),
    correlation_id text NOT NULL,
    occurred_at timestamptz NOT NULL,
    CONSTRAINT auth_security_events_correlation_check CHECK (btrim(correlation_id) <> '')
);

CREATE INDEX auth_security_events_tenant_occurred_idx
    ON modura.auth_security_events (tenant_id, occurred_at DESC);
