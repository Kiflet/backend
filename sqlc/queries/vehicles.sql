-- The vehicles page: each vehicle with its drivers and load, from
-- identity's and shipments' tables. A read model, which moves to reports;
-- vehicles are the fleet module's (internal/modules/fleet).

-- name: ListVehiclesWithDrivers :many
SELECT v.*,
       COALESCE((SELECT string_agg(u.full_name, ', ' ORDER BY u.full_name)
        FROM users u WHERE u.vehicle_id = v.id AND u.is_active), '')::text AS driver_names,
       (SELECT COUNT(*) FROM shipments s
        WHERE s.vehicle_id = v.id
          AND s.status IN ('assigned', 'in_transit', 'delayed', 'arrived', 'issue'))::bigint AS active_shipments,
       EXISTS (SELECT 1 FROM shipments s
               WHERE s.vehicle_id = v.id AND s.tenant_id = v.tenant_id
                 AND s.status IN ('in_transit', 'delayed', 'arrived', 'issue'))::bool AS on_road
FROM vehicles v
WHERE v.tenant_id = $1
ORDER BY v.is_active DESC, v.plate_number;
