-- The drivers' profile: their vehicle, licence and app settings, and the
-- drivers page. These columns of users are the fleet module's
-- (docs/architecture.md, section 4), and move with it.

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

-- name: ListDriversWithStatus :many
-- The drivers page and the assignment picker: who is on duty, how loaded they
-- are, what they carry now, where and when their phone was last heard from,
-- and when their licence runs out.
SELECT u.id, u.full_name, u.phone, u.is_active, u.vehicle_id, u.created_at, u.licence_expires_on,
       v.plate_number AS vehicle_plate,
       sm.last_sync_at, sm.battery_level, sm.tracking_paused, sm.app_version, sm.storage_used_kb,
       COALESCE(sm.power_saving, false)::bool AS power_saving,
       -- On duty (FR-031): carrying a load that has left, or with the app
       -- open and sharing location in the last half hour.
       -- A driver who never synced has no sync row, hence the COALESCEs.
       COALESCE(u.is_active AND (
           EXISTS (SELECT 1 FROM shipments s
                   WHERE s.driver_id = u.id AND s.status IN ('in_transit', 'delayed', 'arrived', 'issue'))
           OR (COALESCE(sm.last_sync_at > NOW() - INTERVAL '30 minutes', false) AND NOT COALESCE(sm.tracking_paused, false))
       ), false)::bool AS on_duty,
       (SELECT COUNT(*) FROM shipments s
        WHERE s.driver_id = u.id
          AND s.status IN ('assigned', 'in_transit', 'delayed', 'arrived', 'issue'))::bigint AS active_shipments,
       (SELECT COUNT(*) FROM shipments s
        WHERE s.driver_id = u.id AND s.status = 'delivered'
          AND s.actual_delivery >= date_trunc('month', NOW()))::bigint AS delivered_this_month,
       -- The load they carry now: one on the road before one waiting.
       COALESCE(cur.tracking_number, '')::text AS current_tracking,
       COALESCE(cur.status, '')::text AS current_status,
       cur.id AS current_shipment_id,
       COALESCE(ST_Y(dp.coordinates), 0)::float8 AS position_lat,
       COALESCE(ST_X(dp.coordinates), 0)::float8 AS position_lng,
       dp.recorded_at AS position_at
FROM users u
LEFT JOIN vehicles v ON v.id = u.vehicle_id
LEFT JOIN sync_metadata sm ON sm.driver_id = u.id
LEFT JOIN driver_positions dp ON dp.driver_id = u.id AND dp.tenant_id = u.tenant_id
LEFT JOIN LATERAL (
    SELECT s.id, s.tracking_number, s.status FROM shipments s
    WHERE s.driver_id = u.id AND s.tenant_id = u.tenant_id
      AND s.status IN ('assigned', 'in_transit', 'delayed', 'arrived', 'issue')
    ORDER BY s.status = 'assigned', s.updated_at DESC
    LIMIT 1
) cur ON true
WHERE u.tenant_id = sqlc.arg(tenant_id)
  AND u.role = 'driver'
  AND (NOT sqlc.arg(active_only)::bool OR u.is_active)
  AND (sqlc.narg(driver_id)::uuid IS NULL OR u.id = sqlc.narg(driver_id)::uuid)
  AND (sqlc.narg(query)::text IS NULL
       OR u.full_name ILIKE '%' || sqlc.narg(query)::text || '%'
       OR u.phone ILIKE '%' || sqlc.narg(query)::text || '%')
ORDER BY u.is_active DESC, u.full_name;

-- name: SetDriverLicence :exec
UPDATE users SET licence_expires_on = sqlc.narg(licence_expires_on)
WHERE id = sqlc.arg(id) AND tenant_id = sqlc.arg(tenant_id) AND role = 'driver';
