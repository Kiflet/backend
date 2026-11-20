-- Sign-ups waiting for their SMS code.

-- name: CreatePendingSignup :exec
-- tenant: none, a sign-up has no company until it is verified.
INSERT INTO pending_signups (token_hash, company, city, full_name, email, phone, password_hash, plan, billing_cycle, expires_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10);

-- name: GetPendingSignup :one
-- tenant: none, a sign-up has no company until it is verified.
SELECT * FROM pending_signups WHERE token_hash = $1 AND expires_at > NOW()
FOR UPDATE;

-- name: DeletePendingSignup :exec
-- tenant: none, a sign-up has no company until it is verified.
DELETE FROM pending_signups WHERE id = $1;

-- name: DeleteExpiredPendingSignups :exec
-- tenant: none, retention, across every company.
DELETE FROM pending_signups WHERE expires_at < $1;
