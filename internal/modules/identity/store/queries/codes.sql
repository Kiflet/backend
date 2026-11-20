-- name: CreateVerificationCode :one
-- tenant: none, codes are sent to a phone before any company is known (sign-in, sign-up).
INSERT INTO verification_codes (phone, purpose, code_hash, expires_at)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetActiveVerificationCode :one
-- tenant: none, codes are sent to a phone before any company is known (sign-in, sign-up).
SELECT * FROM verification_codes
WHERE phone = $1 AND purpose = $2 AND consumed_at IS NULL AND expires_at > NOW()
ORDER BY created_at DESC
LIMIT 1;

-- name: CountVerificationAttempt :one
-- tenant: none, codes are sent to a phone before any company is known (sign-in, sign-up).
-- Counts a try at a code, unless it has had all its tries: then no row.
UPDATE verification_codes SET attempts = attempts + 1
WHERE id = sqlc.arg(id) AND attempts < sqlc.arg(max_attempts)::int
RETURNING attempts;

-- name: ConsumeVerificationCode :execrows
-- tenant: none, codes are sent to a phone before any company is known (sign-in, sign-up).
UPDATE verification_codes SET consumed_at = NOW() WHERE id = $1 AND consumed_at IS NULL;

-- name: LockVerificationSubject :exec
-- tenant: none, codes are sent to a phone before any company is known (sign-in, sign-up).
-- Held until the transaction ends: requests for one number and purpose
-- take turns.
SELECT pg_advisory_xact_lock(hashtext(sqlc.arg(subject)::text));

-- name: CountVerificationCodesSince :one
-- tenant: none, codes are sent to a phone before any company is known (sign-in, sign-up).
SELECT COUNT(*) FROM verification_codes
WHERE phone = $1 AND purpose = $2 AND created_at > $3;

-- name: DeleteExpiredVerificationCodes :exec
-- tenant: none, retention, across every company.
DELETE FROM verification_codes WHERE expires_at < NOW() - INTERVAL '1 day';
