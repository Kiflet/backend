-- Sign-ups waiting for their SMS code.

-- name: CreatePendingSignup :exec
INSERT INTO pending_signups (token_hash, company, city, full_name, email, phone, password_hash, plan, billing_cycle, expires_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10);

-- name: GetPendingSignup :one
SELECT * FROM pending_signups WHERE token_hash = $1 AND expires_at > NOW()
FOR UPDATE;

-- name: DeletePendingSignup :exec
DELETE FROM pending_signups WHERE id = $1;

-- name: DeleteExpiredPendingSignups :exec
DELETE FROM pending_signups WHERE expires_at < $1;
