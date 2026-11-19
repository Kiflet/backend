-- The audit module's own table, audit_logs: append-only, except for a
-- company's deletion (data's purgeTx).

-- name: CreateAuditLog :exec
INSERT INTO audit_logs (
    tenant_id, user_id, action, entity_type, entity_id, old_value, new_value, ip_address, user_agent
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9);

-- name: ListAuditLogsByTenant :many
SELECT * FROM audit_logs
WHERE tenant_id = $1
ORDER BY created_at DESC
LIMIT $2 OFFSET $3;

-- name: SearchAuditLogs :many
-- Every company's log, for the platform admin (AC-042.7): a company's own
-- rows and the platform's actions on it.
SELECT * FROM audit_logs a
WHERE (sqlc.narg(tenant_id)::uuid IS NULL OR a.tenant_id = sqlc.narg(tenant_id) OR a.entity_id = sqlc.narg(tenant_id))
  AND (sqlc.narg(action)::text IS NULL OR a.action = sqlc.narg(action))
  AND (sqlc.narg(ip)::text IS NULL OR a.ip_address = sqlc.narg(ip))
  AND (sqlc.narg(since)::timestamptz IS NULL OR a.created_at >= sqlc.narg(since))
  AND (sqlc.narg(until)::timestamptz IS NULL OR a.created_at < sqlc.narg(until))
ORDER BY a.created_at DESC
LIMIT sqlc.arg(max_rows) OFFSET sqlc.arg(skip);

-- name: DeleteTenantAuditLogs :exec
-- A deleted company's rows; only within purgeTx, which lets the
-- append-only trigger allow it.
DELETE FROM audit_logs WHERE tenant_id = $1;
