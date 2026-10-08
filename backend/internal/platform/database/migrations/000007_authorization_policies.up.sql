-- owner: authorization
ALTER TABLE wheretolive.roles
    ADD COLUMN version bigint NOT NULL DEFAULT 1,
    ADD CONSTRAINT roles_version_positive CHECK (version > 0);

CREATE TABLE wheretolive.role_policies (
    tenant_id uuid NOT NULL,
    role_id uuid NOT NULL,
    resource text NOT NULL,
    action text NOT NULL,
    data_scope text NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, role_id, resource, action),
    CONSTRAINT role_policies_role_fk FOREIGN KEY (tenant_id, role_id)
        REFERENCES wheretolive.roles (tenant_id, id) ON DELETE CASCADE,
    CONSTRAINT role_policies_resource_valid CHECK (resource IN (
        'organization.departments', 'organization.positions',
        'organization.user-organization', 'authorization.roles',
        'authorization.policies', 'authorization.user-roles'
    )),
    CONSTRAINT role_policies_action_valid CHECK (action IN ('read', 'create', 'update', 'delete')),
    CONSTRAINT role_policies_scope_valid CHECK (data_scope IN (
        'all', 'self', 'department', 'department-and-descendants', 'custom'
    ))
);

CREATE TABLE wheretolive.role_policy_departments (
    tenant_id uuid NOT NULL,
    role_id uuid NOT NULL,
    resource text NOT NULL,
    action text NOT NULL,
    department_id uuid NOT NULL,
    PRIMARY KEY (tenant_id, role_id, resource, action, department_id),
    CONSTRAINT role_policy_departments_policy_fk
        FOREIGN KEY (tenant_id, role_id, resource, action)
        REFERENCES wheretolive.role_policies (tenant_id, role_id, resource, action)
        ON DELETE CASCADE,
    CONSTRAINT role_policy_departments_department_fk
        FOREIGN KEY (tenant_id, department_id)
        REFERENCES wheretolive.departments (tenant_id, id)
);

CREATE TABLE wheretolive.user_role_versions (
    tenant_id uuid NOT NULL,
    user_id uuid NOT NULL,
    version bigint NOT NULL DEFAULT 1,
    updated_at timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, user_id),
    CONSTRAINT user_role_versions_user_fk FOREIGN KEY (tenant_id, user_id)
        REFERENCES wheretolive.users (tenant_id, id) ON DELETE CASCADE,
    CONSTRAINT user_role_versions_positive CHECK (version > 0)
);

INSERT INTO wheretolive.role_policies
    (tenant_id, role_id, resource, action, data_scope, created_at, updated_at)
SELECT r.tenant_id, r.id, permission.resource, permission.action, 'all', r.created_at, r.updated_at
FROM wheretolive.roles r
CROSS JOIN (VALUES
    ('organization.departments', 'read'),
    ('organization.departments', 'create'),
    ('organization.departments', 'update'),
    ('organization.departments', 'delete'),
    ('organization.positions', 'read'),
    ('organization.positions', 'create'),
    ('organization.positions', 'update'),
    ('organization.user-organization', 'read'),
    ('organization.user-organization', 'update'),
    ('authorization.roles', 'read'),
    ('authorization.roles', 'create'),
    ('authorization.roles', 'update'),
    ('authorization.roles', 'delete'),
    ('authorization.policies', 'read'),
    ('authorization.policies', 'update'),
    ('authorization.user-roles', 'read'),
    ('authorization.user-roles', 'update')
) AS permission(resource, action)
WHERE r.reserved = true AND r.code = 'tenant-admin';

INSERT INTO wheretolive.user_role_versions (tenant_id, user_id, version, updated_at)
SELECT ur.tenant_id, ur.user_id, 1, max(ur.created_at)
FROM wheretolive.user_roles ur
GROUP BY ur.tenant_id, ur.user_id;
