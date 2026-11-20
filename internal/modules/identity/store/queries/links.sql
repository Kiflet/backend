-- Links sent by email: password resets and address checks.

-- name: CreateEmailToken :exec
INSERT INTO email_tokens (tenant_id, user_id, purpose, email, token_hash, expires_at)
VALUES ($1, $2, $3, $4, $5, $6);

-- name: CountEmailTokensSince :one
SELECT COUNT(*) FROM email_tokens
WHERE tenant_id = $1 AND user_id = $2 AND purpose = $3 AND created_at > $4;

-- name: GetEmailToken :one
-- A link that still works. Whoever follows it is not signed in, so the
-- token itself says which company and person it is for. Whether the
-- company is active is the tenancy module's to say.
SELECT e.id, e.tenant_id, e.user_id, e.email, u.full_name, u.role, u.is_active,
       u.email AS current_email
FROM email_tokens e
JOIN users u ON u.id = e.user_id AND u.tenant_id = e.tenant_id
WHERE e.token_hash = $1 AND e.purpose = $2 AND e.used_at IS NULL AND e.expires_at > NOW()
FOR UPDATE OF e;

-- name: ExpireEmailTokens :exec
-- Once a reset link is used, the others sent to that person stop working.
UPDATE email_tokens SET used_at = NOW()
WHERE tenant_id = $1 AND user_id = $2 AND purpose = $3 AND used_at IS NULL;

-- name: DeleteOldEmailTokens :exec
-- tenant: none, retention, across every company.
DELETE FROM email_tokens WHERE created_at < $1;
