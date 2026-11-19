-- The tenancy module's own table, tenants: the companies. Billing's plan
-- and standing columns are still written by internal/service until the
-- billing module moves (docs/migration.md, phase 1).

-- name: GetTenantByID :one
SELECT * FROM tenants WHERE id = $1;

-- name: CreateTenant :one
-- A slug already taken returns no row, so the caller can try another
-- without aborting its transaction.
INSERT INTO tenants (name, slug, plan, max_drivers, contact_phone, contact_email, region_city, trial_ends_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT (slug) DO NOTHING
RETURNING *;

-- name: UpdateTenantSettings :one
UPDATE tenants
SET name = $2,
    contact_phone = $3,
    contact_email = $4,
    region_city = $5,
    max_active_per_driver = $6,
    sms_on_created = $7,
    sms_on_dispatched = $8,
    sms_on_arrived = $9,
    sms_on_delivered = $10,
    sms_on_cancelled = $11,
    sms_on_delayed = $12,
    sms_driver_on_assign = $13,
    tin = $14,
    alert_on_delivery = $15,
    customer_language = $16,
    updated_at = NOW()
WHERE id = $1
RETURNING *;

-- name: SetTenantActive :exec
UPDATE tenants SET is_active = $2 WHERE id = $1;
