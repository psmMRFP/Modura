-- owner: authorization
DROP TABLE IF EXISTS wheretolive.user_role_versions;
DROP TABLE IF EXISTS wheretolive.role_policy_departments;
DROP TABLE IF EXISTS wheretolive.role_policies;
ALTER TABLE wheretolive.roles DROP CONSTRAINT IF EXISTS roles_version_positive;
ALTER TABLE wheretolive.roles DROP COLUMN IF EXISTS version;
