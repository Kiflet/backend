-- The identity module's people: who they are, their role and how they
-- sign in. The driver profile columns (vehicle_id, licence_expires_on and
-- the app settings) are fleet's: sqlc/queries/drivers.sql until it moves.

-- name: GetUserByID :one
SELECT * FROM users
WHERE id = $1 AND tenant_id = $2;

-- name: GetUserByPhone :one
SELECT * FROM users
WHERE phone = $1;

-- name: GetUserByEmail :one
SELECT * FROM users
WHERE lower(email) = lower(sqlc.arg(email));

-- name: ListUsersByTenant :many
SELECT * FROM users
WHERE tenant_id = $1 AND role <> 'driver'
ORDER BY is_active DESC, full_name
LIMIT $2 OFFSET $3;

-- name: CountStaffByTenant :one
SELECT COUNT(*) FROM users WHERE tenant_id = $1 AND role <> 'driver';

-- name: CreateUser :one
INSERT INTO users (
    tenant_id, role, full_name, phone, email, password_hash, pin_hash,
    is_active, password_reset_required, vehicle_id
) VALUES ($1, $2, $3, $4, $5, $6, $7, true, $8, $9)
RETURNING *;

-- name: UpdateUserProfile :one
-- A changed email address has to be confirmed again.
UPDATE users
SET full_name = $3,
    phone = $4,
    email = $5,
    email_verified_at = CASE WHEN email IS NOT DISTINCT FROM $5 THEN email_verified_at END,
    updated_at = NOW()
WHERE id = $1 AND tenant_id = $2
RETURNING *;

-- name: SetUserRole :exec
UPDATE users SET role = $3 WHERE id = $1 AND tenant_id = $2 AND role <> 'driver';

-- name: SetUserActive :exec
UPDATE users SET is_active = $3 WHERE id = $1 AND tenant_id = $2;

-- name: CountActiveDriversByTenant :one
SELECT COUNT(*) FROM users WHERE tenant_id = $1 AND role = 'driver' AND is_active;

-- name: IncrementFailedLoginAttempts :one
UPDATE users
SET failed_login_attempts = failed_login_attempts + 1
WHERE id = $1
RETURNING *;

-- name: RecordSuccessfulLogin :exec
UPDATE users
SET failed_login_attempts = 0, locked_until = NULL, last_login_at = NOW()
WHERE id = $1;

-- name: LockUserAccount :exec
-- Locking clears the count: when the lock expires the account starts again
-- from zero, instead of one mistype locking it at once.
UPDATE users
SET locked_until = $2, failed_login_attempts = 0
WHERE id = $1;

-- name: UpdatePassword :exec
UPDATE users
SET password_hash = $2, password_reset_required = $3, failed_login_attempts = 0, locked_until = NULL
WHERE id = $1;

-- name: UpdatePIN :exec
UPDATE users
SET pin_hash = $2, password_reset_required = $3, failed_login_attempts = 0, locked_until = NULL
WHERE id = $1;

-- name: UserNamesByIDs :many
-- Names of the people in the platform's audit log, from every company:
-- for the platform admin only (UserService.Names checks).
SELECT id, full_name FROM users WHERE id = ANY(sqlc.arg(ids)::uuid[]);

-- name: SetEmailVerified :execrows
UPDATE users SET email_verified_at = NOW(), updated_at = NOW()
WHERE id = $1 AND tenant_id = $2 AND email = sqlc.arg(email) AND email_verified_at IS NULL;

-- name: ResetUserPassword :exec
-- Following a reset link proves the address, and ends any lockout.
UPDATE users
SET password_hash = $3, password_reset_required = false,
    failed_login_attempts = 0, locked_until = NULL,
    email_verified_at = COALESCE(email_verified_at, NOW()), updated_at = NOW()
WHERE id = $1 AND tenant_id = $2;
