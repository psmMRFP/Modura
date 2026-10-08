-- owner: authorization
DELETE FROM wheretolive.role_policies
WHERE resource IN ('settings.dictionaries', 'settings.configurations', 'audit.events');
ALTER TABLE wheretolive.role_policies DROP CONSTRAINT role_policies_resource_valid;
ALTER TABLE wheretolive.role_policies ADD CONSTRAINT role_policies_resource_valid CHECK (resource IN (
    'organization.departments', 'organization.positions',
    'organization.user-organization', 'authorization.roles',
    'authorization.policies', 'authorization.user-roles'
));

-- owner: settings
DROP TABLE IF EXISTS wheretolive.tenant_configuration_values;
DROP TABLE IF EXISTS wheretolive.global_configuration_values;
DROP TABLE IF EXISTS wheretolive.configuration_definitions;
DROP TABLE IF EXISTS wheretolive.tenant_dictionary_items;
DROP TABLE IF EXISTS wheretolive.tenant_dictionary_types;
DROP TABLE IF EXISTS wheretolive.global_dictionary_items;
DROP TABLE IF EXISTS wheretolive.global_dictionary_types;
