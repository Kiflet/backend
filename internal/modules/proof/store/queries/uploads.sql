-- The proof module's uploads: files a driver's app is about to store.

-- name: PrepareUpload :one
-- The same driver asking again for the same file gets the row they had.
INSERT INTO uploads (tenant_id, driver_id, kind, storage_key, content_type, size_bytes, md5)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (tenant_id, driver_id, kind, md5, size_bytes) DO UPDATE SET kind = EXCLUDED.kind
RETURNING *;

-- name: GetUpload :one
SELECT * FROM uploads WHERE tenant_id = $1 AND id = $2;

-- name: MarkUploadStored :exec
UPDATE uploads SET stored_at = now() WHERE tenant_id = $1 AND id = $2 AND stored_at IS NULL;

-- name: ListTenantUploadKeys :many
-- A company's files, removed from storage when it is deleted.
SELECT storage_key FROM uploads WHERE tenant_id = $1;

-- name: GetDriverUploadByKey :one
SELECT * FROM uploads WHERE tenant_id = $1 AND driver_id = $2 AND storage_key = $3;
