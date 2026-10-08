-- name: InsertTenantAuditEvent :exec
INSERT INTO wheretolive.audit_events
    (id, actor_type, actor_id, tenant_id, action, resource, resource_id, reason, result, correlation_id, occurred_at, before_state, after_state)
VALUES ($1, 'tenant_user', $2, $3, $4, $5, $6, $7, 'succeeded', $8, $9, $10, $11);

-- name: InsertPlatformAuditEvent :exec
INSERT INTO wheretolive.audit_events
    (id, actor_type, actor_id, tenant_id, action, resource, resource_id, reason, result, correlation_id, occurred_at, before_state, after_state)
VALUES ($1, 'platform_administrator', $2, NULL, $3, $4, $5, $6, 'succeeded', $7, $8, $9, $10);

-- name: InsertTenantScopedPlatformAuditEvent :exec
INSERT INTO wheretolive.audit_events
    (id, actor_type, actor_id, tenant_id, action, resource, resource_id, reason, result, correlation_id, occurred_at, before_state, after_state)
VALUES ($1, 'platform_administrator', $2, $3, $4, $5, $6, $7, 'succeeded', $8, $9, $10, $11);
