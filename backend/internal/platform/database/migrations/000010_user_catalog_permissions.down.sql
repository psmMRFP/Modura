-- owner: authorization
DELETE FROM modura.role_policies WHERE resource = 'identity.users';
ALTER TABLE modura.role_policies DROP CONSTRAINT role_policies_resource_valid;
ALTER TABLE modura.role_policies ADD CONSTRAINT role_policies_resource_valid CHECK (resource IN (
    'organization.departments', 'organization.positions', 'organization.user-organization',
    'authorization.roles', 'authorization.policies', 'authorization.user-roles',
    'settings.dictionaries', 'settings.configurations', 'audit.events'
));
