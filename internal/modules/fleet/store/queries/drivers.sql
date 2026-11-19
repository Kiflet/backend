-- The drivers' profile: the fleet module's columns of users (vehicle_id,
-- licence_expires_on and the app settings). The rest of users is the
-- identity module's. ListPapersDue reads its role and standing to leave
-- out drivers who have gone, a read of identity's columns kept on purpose:
-- the two share the row.

-- name: SetDriverVehicle :exec
UPDATE users SET vehicle_id = $3 WHERE id = $1 AND tenant_id = $2 AND role = 'driver';

-- name: ClearVehicleFromDrivers :exec
UPDATE users SET vehicle_id = NULL WHERE vehicle_id = $1 AND tenant_id = $2;

-- name: SetDriverAppSettings :exec
-- The driver app's settings, chosen on the phone (FR-026 AC-026.6). The
-- phone sends only those the driver changed; the rest stay as they are.
UPDATE users
SET language = COALESCE(sqlc.narg(language)::text, language),
    app_battery_saver = COALESCE(sqlc.narg(battery_saver)::boolean, app_battery_saver),
    app_vibrate = COALESCE(sqlc.narg(vibrate)::boolean, app_vibrate)
WHERE id = sqlc.arg(id) AND tenant_id = sqlc.arg(tenant_id) AND role = 'driver';

-- name: SetDriverLicence :exec
UPDATE users SET licence_expires_on = sqlc.narg(licence_expires_on)
WHERE id = sqlc.arg(id) AND tenant_id = sqlc.arg(tenant_id) AND role = 'driver';

-- name: ListPapersDue :many
-- The papers check: every active driver's licence and every vehicle's
-- insurance and inspection that runs out on or before a day, across
-- companies. The alert's episode names the date, so each is raised once.
SELECT 'licence'::text AS kind, u.tenant_id, u.id AS subject_id, u.full_name AS name, u.licence_expires_on AS due_on
FROM users u
WHERE u.role = 'driver' AND u.is_active AND u.anonymised_at IS NULL
  AND u.licence_expires_on <= sqlc.arg(until)::date
UNION ALL
SELECT 'insurance', v.tenant_id, v.id, v.plate_number, v.insurance_expires_on
FROM vehicles v
WHERE v.is_active AND v.insurance_expires_on <= sqlc.arg(until)::date
UNION ALL
SELECT 'inspection', v.tenant_id, v.id, v.plate_number, v.inspection_due_on
FROM vehicles v
WHERE v.is_active AND v.inspection_due_on <= sqlc.arg(until)::date;
