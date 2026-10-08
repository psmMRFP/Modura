-- owner: settings
CREATE TABLE wheretolive.global_dictionary_types (
    id uuid PRIMARY KEY,
    code text NOT NULL UNIQUE,
    name text NOT NULL,
    version bigint NOT NULL DEFAULT 1,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CONSTRAINT global_dictionary_types_code_normalized CHECK (code = lower(btrim(code)) AND code <> ''),
    CONSTRAINT global_dictionary_types_name_not_blank CHECK (btrim(name) <> ''),
    CONSTRAINT global_dictionary_types_version_positive CHECK (version > 0)
);

CREATE TABLE wheretolive.global_dictionary_items (
    id uuid PRIMARY KEY,
    dictionary_type_id uuid NOT NULL REFERENCES wheretolive.global_dictionary_types (id) ON DELETE CASCADE,
    code text NOT NULL,
    label text NOT NULL,
    sort_order integer NOT NULL,
    enabled boolean NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CONSTRAINT global_dictionary_items_code_normalized CHECK (code = lower(btrim(code)) AND code <> ''),
    CONSTRAINT global_dictionary_items_label_not_blank CHECK (btrim(label) <> ''),
    CONSTRAINT global_dictionary_items_code_unique UNIQUE (dictionary_type_id, code)
);

CREATE TABLE wheretolive.tenant_dictionary_types (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL REFERENCES wheretolive.tenants (id),
    code text NOT NULL,
    name text NOT NULL,
    version bigint NOT NULL DEFAULT 1,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CONSTRAINT tenant_dictionary_types_tenant_id_id_unique UNIQUE (tenant_id, id),
    CONSTRAINT tenant_dictionary_types_code_normalized CHECK (code = lower(btrim(code)) AND code <> ''),
    CONSTRAINT tenant_dictionary_types_name_not_blank CHECK (btrim(name) <> ''),
    CONSTRAINT tenant_dictionary_types_version_positive CHECK (version > 0),
    CONSTRAINT tenant_dictionary_types_code_unique UNIQUE (tenant_id, code)
);

CREATE TABLE wheretolive.tenant_dictionary_items (
    id uuid PRIMARY KEY,
    tenant_id uuid NOT NULL,
    dictionary_type_id uuid NOT NULL,
    code text NOT NULL,
    label text NOT NULL,
    sort_order integer NOT NULL,
    enabled boolean NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CONSTRAINT tenant_dictionary_items_type_fk FOREIGN KEY (tenant_id, dictionary_type_id)
        REFERENCES wheretolive.tenant_dictionary_types (tenant_id, id) ON DELETE CASCADE,
    CONSTRAINT tenant_dictionary_items_code_normalized CHECK (code = lower(btrim(code)) AND code <> ''),
    CONSTRAINT tenant_dictionary_items_label_not_blank CHECK (btrim(label) <> ''),
    CONSTRAINT tenant_dictionary_items_code_unique UNIQUE (tenant_id, dictionary_type_id, code)
);

CREATE TABLE wheretolive.configuration_definitions (
    id uuid PRIMARY KEY,
    key text NOT NULL UNIQUE,
    name text NOT NULL,
    value_type text NOT NULL,
    tenant_overridable boolean NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CONSTRAINT configuration_definitions_key_normalized CHECK (key = lower(btrim(key)) AND key <> ''),
    CONSTRAINT configuration_definitions_name_not_blank CHECK (btrim(name) <> ''),
    CONSTRAINT configuration_definitions_type_valid CHECK (value_type IN ('string', 'boolean', 'integer', 'json'))
);

CREATE TABLE wheretolive.global_configuration_values (
    key text PRIMARY KEY REFERENCES wheretolive.configuration_definitions (key) ON DELETE CASCADE,
    value jsonb NOT NULL,
    version bigint NOT NULL DEFAULT 1,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CONSTRAINT global_configuration_values_version_positive CHECK (version > 0)
);

CREATE TABLE wheretolive.tenant_configuration_values (
    tenant_id uuid NOT NULL REFERENCES wheretolive.tenants (id),
    key text NOT NULL REFERENCES wheretolive.configuration_definitions (key) ON DELETE CASCADE,
    value jsonb NOT NULL,
    version bigint NOT NULL DEFAULT 1,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    PRIMARY KEY (tenant_id, key),
    CONSTRAINT tenant_configuration_values_version_positive CHECK (version > 0)
);

-- owner: authorization
ALTER TABLE wheretolive.role_policies DROP CONSTRAINT role_policies_resource_valid;
ALTER TABLE wheretolive.role_policies ADD CONSTRAINT role_policies_resource_valid CHECK (resource IN (
    'organization.departments', 'organization.positions',
    'organization.user-organization', 'authorization.roles',
    'authorization.policies', 'authorization.user-roles',
    'settings.dictionaries', 'settings.configurations', 'audit.events'
));

INSERT INTO wheretolive.role_policies
    (tenant_id, role_id, resource, action, data_scope, created_at, updated_at)
SELECT r.tenant_id, r.id, permission.resource, permission.action, 'all', r.created_at, r.updated_at
FROM wheretolive.roles r
CROSS JOIN (VALUES
    ('settings.dictionaries', 'read'),
    ('settings.dictionaries', 'create'),
    ('settings.dictionaries', 'update'),
    ('settings.dictionaries', 'delete'),
    ('settings.configurations', 'read'),
    ('settings.configurations', 'update'),
    ('audit.events', 'read')
) AS permission(resource, action)
WHERE r.reserved = true AND r.code = 'tenant-admin';
