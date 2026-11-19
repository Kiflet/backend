-- The people running Kiflet: every company's audit log, support tickets
-- and the background jobs (FR-042 AC-042.7, FR-049).

-- name: CreateSupportTicket :one
INSERT INTO support_tickets (tenant_id, subject, opened_by)
VALUES ($1, $2, $3)
RETURNING *;

-- name: AddSupportMessage :one
INSERT INTO support_messages (ticket_id, tenant_id, author_id, author_name, from_platform, body)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: SetSupportTicketStatus :exec
UPDATE support_tickets
SET status = sqlc.arg(status)::text, updated_at = NOW(),
    closed_at = CASE WHEN sqlc.arg(status)::text = 'closed' THEN NOW() END
WHERE id = sqlc.arg(id) AND tenant_id = sqlc.arg(tenant_id);

-- name: ListCompanyTickets :many
SELECT t.id, t.number, t.subject, t.status, t.created_at, t.updated_at, t.closed_at,
       COALESCE(u.full_name, '')::text AS opened_by_name,
       (SELECT COUNT(*) FROM support_messages m WHERE m.ticket_id = t.id)::bigint AS messages
FROM support_tickets t
LEFT JOIN users u ON u.id = t.opened_by
WHERE t.tenant_id = $1
ORDER BY t.status = 'closed', t.updated_at DESC
LIMIT 200;

-- name: GetCompanyTicket :one
SELECT t.*, COALESCE(u.full_name, '')::text AS opened_by_name, COALESCE(u.email, '')::text AS opened_by_email
FROM support_tickets t
LEFT JOIN users u ON u.id = t.opened_by
WHERE t.id = $1 AND t.tenant_id = $2;

-- name: ListTicketMessages :many
SELECT * FROM support_messages
WHERE ticket_id = $1 AND tenant_id = $2
ORDER BY created_at;

-- name: ListSupportInbox :many
-- Every company's tickets for the platform: waiting for an answer first.
SELECT t.id, t.tenant_id, t.number, t.subject, t.status, t.created_at, t.updated_at,
       tn.name AS tenant_name, COALESCE(u.full_name, '')::text AS opened_by_name,
       (SELECT COUNT(*) FROM support_messages m WHERE m.ticket_id = t.id)::bigint AS messages
FROM support_tickets t
JOIN tenants tn ON tn.id = t.tenant_id
LEFT JOIN users u ON u.id = t.opened_by
WHERE (sqlc.narg(status)::text IS NULL OR t.status = sqlc.narg(status))
ORDER BY CASE t.status WHEN 'open' THEN 0 WHEN 'answered' THEN 1 ELSE 2 END, t.updated_at DESC
LIMIT 200;

-- name: GetTicketForPlatform :one
-- Only for the platform admin, who answers every company.
SELECT t.*, tn.name AS tenant_name, COALESCE(u.full_name, '')::text AS opened_by_name,
       COALESCE(u.email, '')::text AS opened_by_email
FROM support_tickets t
JOIN tenants tn ON tn.id = t.tenant_id
LEFT JOIN users u ON u.id = t.opened_by
WHERE t.id = $1;

-- name: CountWaitingTickets :one
SELECT COUNT(*) FROM support_tickets WHERE status = 'open';

-- name: ListPlatformAdmins :many
SELECT full_name, email::text AS email FROM users
WHERE role = 'platform_admin' AND is_active AND email IS NOT NULL
ORDER BY created_at;

-- name: RecordJobRun :exec
INSERT INTO job_runs (name, started_at, duration_ms, error, last_ok_at, runs, failures)
VALUES (sqlc.arg(name), sqlc.arg(started_at), sqlc.arg(duration_ms), sqlc.narg(error),
        CASE WHEN sqlc.narg(error)::text IS NULL THEN sqlc.arg(started_at)::timestamptz END, 1,
        CASE WHEN sqlc.narg(error)::text IS NULL THEN 0 ELSE 1 END)
ON CONFLICT (name) DO UPDATE
SET started_at = EXCLUDED.started_at, duration_ms = EXCLUDED.duration_ms, error = EXCLUDED.error,
    last_ok_at = COALESCE(EXCLUDED.last_ok_at, job_runs.last_ok_at),
    runs = job_runs.runs + 1, failures = job_runs.failures + EXCLUDED.failures;

-- name: ListJobRuns :many
SELECT * FROM job_runs ORDER BY name;

-- name: OutboxHealth :many
-- Texts and emails waiting, and those sent or failed in the last day.
SELECT 'sms'::text AS channel, status, COUNT(*)::bigint AS messages, MIN(created_at)::timestamptz AS oldest
FROM sms_messages
WHERE direction = 'outbound' AND (status IN ('queued', 'sending') OR created_at > NOW() - INTERVAL '24 hours')
GROUP BY status
UNION ALL
SELECT 'email'::text, status, COUNT(*)::bigint, MIN(created_at)::timestamptz
FROM emails
WHERE status IN ('queued', 'sending') OR created_at > NOW() - INTERVAL '24 hours'
GROUP BY status
ORDER BY 1, 2;

-- name: OutboxWaiting :many
-- Texts and emails waiting to be sent, for /metrics. Read off the partial
-- queue indexes, so it is cheap to run every minute.
SELECT 'sms'::text AS channel, COUNT(*)::bigint AS waiting, MIN(created_at)::timestamptz AS oldest
FROM sms_messages WHERE status = 'queued'
UNION ALL
SELECT 'email'::text, COUNT(*)::bigint, MIN(created_at)::timestamptz
FROM emails WHERE status = 'queued';

-- name: LeaseJob :one
-- Takes a timed job's lease unless another worker holds it. true when
-- taken.
INSERT INTO job_leases (name, until) VALUES (sqlc.arg(name), NOW() + sqlc.arg(lease)::interval)
ON CONFLICT (name) DO UPDATE SET until = EXCLUDED.until WHERE job_leases.until < NOW()
RETURNING true AS taken;

-- name: ReleaseJob :exec
UPDATE job_leases SET until = NOW() WHERE name = sqlc.arg(name);
