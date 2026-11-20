-- name: ClaimOutboxEvents :many
-- The next events to publish, oldest first. Run in a transaction that
-- marks them sent or failed before it commits; SKIP LOCKED lets a second
-- relay take others instead of waiting.
SELECT id, tenant_id, type, version, exchange, routing_key, payload, trace_id, created_at, attempts
FROM event_outbox
WHERE sent_at IS NULL AND available_at <= now()
ORDER BY created_at
LIMIT sqlc.arg(max_events)
FOR UPDATE SKIP LOCKED;

-- name: MarkOutboxSent :exec
-- tenant: none, the event relay, which publishes every company's events.
UPDATE event_outbox SET sent_at = now(), attempts = attempts + 1, last_error = NULL
WHERE id = ANY(sqlc.arg(ids)::uuid[]);

-- name: MarkOutboxFailed :exec
-- tenant: none, the event relay, which publishes every company's events.
-- A publish that failed is tried again after delay.
UPDATE event_outbox
SET attempts = attempts + 1, last_error = sqlc.arg(last_error), available_at = now() + make_interval(secs => sqlc.arg(delay_seconds)::float8)
WHERE id = ANY(sqlc.arg(ids)::uuid[]);

-- name: OutboxLag :one
-- tenant: none, the event relay, which publishes every company's events.
-- How many events wait, and how long the oldest has, for /metrics.
SELECT count(*)::int AS waiting,
       COALESCE(EXTRACT(EPOCH FROM now() - min(created_at)), 0)::float8 AS oldest_seconds
FROM event_outbox
WHERE sent_at IS NULL;

-- name: DeleteSentOutboxEvents :exec
-- tenant: none, retention, across every company.
DELETE FROM event_outbox WHERE sent_at < sqlc.arg(before);

-- name: MarkMessageProcessed :execrows
-- tenant: none, a consumer's record of messages it handled, across every company.
-- A consumer's record of a message it handled, in the same transaction as
-- its effect. No row: it was handled already, so the handler does nothing.
INSERT INTO processed_messages (consumer, message_id)
VALUES (sqlc.arg(consumer), sqlc.arg(message_id))
ON CONFLICT DO NOTHING;

-- name: DeleteOldProcessedMessages :exec
-- tenant: none, retention, across every company.
DELETE FROM processed_messages WHERE processed_at < sqlc.arg(before);
