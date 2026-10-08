-- owner: authorization
CREATE TABLE wheretolive.roles (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL REFERENCES wheretolive.tenants (id),
    code text NOT NULL,
    name text NOT NULL,
    reserved boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CONSTRAINT roles_tenant_id_id_unique UNIQUE (tenant_id, id),
    CONSTRAINT roles_code_normalized CHECK (code = lower(btrim(code)) AND code <> ''),
    CONSTRAINT roles_code_unique UNIQUE (tenant_id, code)
);
CREATE TABLE wheretolive.user_roles (
    tenant_id uuid NOT NULL,
    user_id uuid NOT NULL,
    role_id uuid NOT NULL,
    created_at timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, user_id, role_id),
    CONSTRAINT user_roles_user_fk FOREIGN KEY (tenant_id, user_id)
        REFERENCES wheretolive.users (tenant_id, id),
    CONSTRAINT user_roles_role_fk FOREIGN KEY (tenant_id, role_id)
        REFERENCES wheretolive.roles (tenant_id, id)
);

-- owner: provisioning
CREATE TABLE wheretolive.tenant_provisioning_requests (
    idempotency_key uuid PRIMARY KEY,
    request_digest bytea NOT NULL,
    tenant_id uuid NOT NULL UNIQUE REFERENCES wheretolive.tenants (id),
    created_at timestamptz NOT NULL,
    completed_at timestamptz NOT NULL,
    CONSTRAINT tenant_provisioning_digest_length CHECK (octet_length(request_digest) = 32)
);
