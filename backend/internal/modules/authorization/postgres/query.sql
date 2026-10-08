-- Authorization-owned queries. The users table belongs to the identity
-- module; account existence is verified through identity's public API.

-- name: UserRoleVersion :one
SELECT version
FROM wheretolive.user_role_versions
WHERE tenant_id = $1 AND user_id = $2;

-- name: LockUserRoleVersion :one
SELECT version
FROM wheretolive.user_role_versions
WHERE tenant_id = $1 AND user_id = $2
FOR UPDATE;

-- name: InitializeUserRoleVersion :execrows
INSERT INTO wheretolive.user_role_versions (tenant_id, user_id, version, updated_at)
VALUES ($1, $2, 1, $3)
ON CONFLICT (tenant_id, user_id) DO NOTHING;

-- name: NonReservedRoleIDsByUser :many
SELECT ur.role_id
FROM wheretolive.user_roles ur
JOIN wheretolive.roles r ON r.tenant_id = ur.tenant_id AND r.id = ur.role_id
WHERE ur.tenant_id = $1 AND ur.user_id = $2 AND r.reserved = false
ORDER BY ur.role_id;
