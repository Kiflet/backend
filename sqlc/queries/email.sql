-- name: EnqueueEmail :one
INSERT INTO emails (tenant_id, to_address, subject, body, purpose, sensitive)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: ClaimQueuedEmails :many
-- tenant: none, the outbound queue, which sends every company's mail.
-- The same five-minute lease as ClaimQueuedSMS.
UPDATE emails
SET status = 'sending', attempts = attempts + 1, next_attempt_at = NOW() + INTERVAL '5 minutes'
WHERE id IN (
    SELECT id FROM emails
    WHERE status = 'queued' AND next_attempt_at <= NOW()
    ORDER BY next_attempt_at
    LIMIT $1
    FOR UPDATE SKIP LOCKED
)
RETURNING *;

-- name: MarkEmailSent :exec
-- tenant: none, the outbound queue, which sends every company's mail.
UPDATE emails
SET status = 'sent',
    sent_at = NOW(),
    provider = $2,
    last_error = NULL,
    body = CASE WHEN sensitive THEN '[redacted]' ELSE body END
WHERE id = $1;

-- name: MarkEmailRetry :exec
-- tenant: none, the outbound queue, which sends every company's mail.
UPDATE emails
SET status = 'queued', last_error = $2, next_attempt_at = $3
WHERE id = $1;

-- name: MarkEmailFailed :exec
-- tenant: none, the outbound queue, which sends every company's mail.
UPDATE emails
SET status = 'failed',
    last_error = $2,
    body = CASE WHEN sensitive THEN '[redacted]' ELSE body END
WHERE id = $1;

-- name: ReleaseEmails :exec
-- tenant: none, the outbound queue, which sends every company's mail.
-- As ReleaseSMS.
UPDATE emails
SET status = 'queued', attempts = attempts - 1, next_attempt_at = NOW()
WHERE id = ANY(sqlc.arg(ids)::uuid[]) AND status = 'sending';

-- name: RequeueStuckEmails :exec
-- tenant: none, the outbound queue, which sends every company's mail.
UPDATE emails
SET status = 'queued'
WHERE status = 'sending' AND next_attempt_at < NOW();

-- name: DeleteOldEmails :exec
-- tenant: none, retention, across every company.
DELETE FROM emails WHERE created_at < $1;

-- name: ClaimEmailsByIDs :many
-- tenant: none, the outbound queue, which sends every company's mail.
-- As ClaimSMSByIDs (notify.email).
UPDATE emails
SET status = 'sending', attempts = attempts + 1, next_attempt_at = NOW() + INTERVAL '5 minutes'
WHERE id IN (
    SELECT id FROM emails
    WHERE id = ANY(sqlc.arg(ids)::uuid[]) AND status = 'queued' AND next_attempt_at <= NOW()
    FOR UPDATE SKIP LOCKED
)
RETURNING *;

-- name: DueEmailIDs :many
-- tenant: none, the outbound queue, which sends every company's mail.
SELECT id FROM emails
WHERE status = 'queued' AND next_attempt_at <= NOW()
ORDER BY next_attempt_at
LIMIT sqlc.arg(max_messages);
