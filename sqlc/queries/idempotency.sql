-- name: ClaimIdempotencyKey :execrows
-- The first request with a key claims it. A key past its 24 hours, or one
-- whose request never finished (the process died), is claimed afresh.
INSERT INTO idempotency_keys (tenant_id, caller_id, key, request_hash)
VALUES (sqlc.arg(tenant_id), sqlc.arg(caller_id), sqlc.arg(key), sqlc.arg(request_hash))
ON CONFLICT (tenant_id, caller_id, key) DO UPDATE
SET request_hash = EXCLUDED.request_hash, response_status = NULL, response_type = NULL,
    response_body = NULL, created_at = now(), completed_at = NULL
WHERE idempotency_keys.created_at < sqlc.arg(expired_before)
   OR (idempotency_keys.completed_at IS NULL AND idempotency_keys.created_at < sqlc.arg(abandoned_before));

-- name: GetIdempotencyKey :one
SELECT request_hash, response_status, COALESCE(response_type, '')::text AS response_type, response_body
FROM idempotency_keys
WHERE tenant_id = sqlc.arg(tenant_id) AND caller_id = sqlc.arg(caller_id) AND key = sqlc.arg(key);

-- name: SaveIdempotentResponse :exec
UPDATE idempotency_keys
SET response_status = sqlc.arg(response_status), response_type = sqlc.arg(response_type),
    response_body = sqlc.arg(response_body), completed_at = now()
WHERE tenant_id = sqlc.arg(tenant_id) AND caller_id = sqlc.arg(caller_id) AND key = sqlc.arg(key)
  AND request_hash = sqlc.arg(request_hash) AND completed_at IS NULL;

-- name: ReleaseIdempotencyKey :exec
-- A request that failed on our side gives its key back, so the retry runs.
DELETE FROM idempotency_keys
WHERE tenant_id = sqlc.arg(tenant_id) AND caller_id = sqlc.arg(caller_id) AND key = sqlc.arg(key)
  AND request_hash = sqlc.arg(request_hash) AND completed_at IS NULL;

-- name: DeleteOldIdempotencyKeys :exec
-- tenant: none, retention, across every company.
DELETE FROM idempotency_keys WHERE created_at < sqlc.arg(before);
