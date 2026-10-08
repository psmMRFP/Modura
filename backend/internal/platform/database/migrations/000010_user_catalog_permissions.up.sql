-- owner: authorization
ALTER TABLE wheretolive.role_policies DROP CONSTRAINT role_policies_resource_valid;
ALTER TABLE wheretolive.role_policies ADD CONSTRAINT role_policies_resource_valid CHECK (resource IN (
    'identity.users', 'organization.departments', 'organization.positions',
    'organization.user-organization', 'authorization.roles', 'authorization.policies',
    'authorization.user-roles', 'settings.dictionaries', 'settings.configurations', 'audit.events'
));

INSERT INTO wheretolive.role_policies (tenant_id, role_id, resource, action, data_scope, created_at, updated_at)
SELECT r.tenant_id, r.id, 'identity.users', permission.action, 'all', r.created_at, r.updated_at
FROM wheretolive.roles r
CROSS JOIN (VALUES ('read'), ('update')) AS permission(action)
WHERE r.reserved = true AND r.code = 'tenant-admin'
ON CONFLICT DO NOTHING;
