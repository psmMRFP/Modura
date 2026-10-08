-- owner: audit
CREATE TABLE wheretolive.audit_events (
    id uuid PRIMARY KEY,
    actor_type text NOT NULL,
    actor_id uuid NOT NULL,
    tenant_id uuid REFERENCES wheretolive.tenants (id),
    action text NOT NULL,
    resource text NOT NULL,
    resource_id uuid NOT NULL,
    reason text NOT NULL,
    result text NOT NULL,
    correlation_id text NOT NULL,
    occurred_at timestamptz NOT NULL,
    CONSTRAINT audit_events_actor_type_check CHECK (actor_type IN ('platform_administrator', 'tenant_user')),
    CONSTRAINT audit_events_result_check CHECK (result IN ('succeeded', 'failed')),
    CONSTRAINT audit_events_reason_not_blank CHECK (btrim(reason) <> ''),
    CONSTRAINT audit_events_correlation_not_blank CHECK (btrim(correlation_id) <> '')
);

CREATE INDEX audit_events_tenant_occurred_idx
    ON wheretolive.audit_events (tenant_id, occurred_at DESC);
