-- Shipments with what other modules know of them (who drives, which
-- vehicle, positions, alert rules, proofs): read models for the lists,
-- the map and the timed checks, which move to reports. The shipments
-- table is the shipments module's (internal/modules/shipments).

-- name: ListShipments :many
-- Every filter is optional: an empty status list or a NULL argument means "no
-- constraint". Sorting goes through a whitelist of keys rather than
-- interpolated SQL.
SELECT sqlc.embed(s),
       d.full_name AS driver_name,
       v.plate_number AS vehicle_plate,
       sm.last_sync_at AS driver_last_sync
FROM shipments s
LEFT JOIN users d ON d.id = s.driver_id
LEFT JOIN vehicles v ON v.id = s.vehicle_id
LEFT JOIN sync_metadata sm ON sm.driver_id = s.driver_id
WHERE s.tenant_id = sqlc.arg(tenant_id)
  AND (cardinality(sqlc.arg(statuses)::text[]) = 0 OR s.status = ANY(sqlc.arg(statuses)::text[]))
  AND (sqlc.narg(driver_id)::uuid IS NULL OR s.driver_id = sqlc.narg(driver_id)::uuid)
  AND (sqlc.narg(vehicle_id)::uuid IS NULL OR s.vehicle_id = sqlc.narg(vehicle_id)::uuid)
  AND (sqlc.narg(priority)::text IS NULL OR s.priority = sqlc.narg(priority)::text)
  AND (sqlc.narg(date_from)::timestamptz IS NULL OR s.created_at >= sqlc.narg(date_from)::timestamptz)
  AND (sqlc.narg(date_to)::timestamptz IS NULL OR s.created_at < sqlc.narg(date_to)::timestamptz)
  AND (sqlc.narg(origin)::text IS NULL
       OR s.origin_city ILIKE sqlc.narg(origin)::text
       OR s.origin_address ILIKE '%' || sqlc.narg(origin)::text || '%')
  AND (sqlc.narg(destination)::text IS NULL
       OR s.destination_city ILIKE sqlc.narg(destination)::text
       OR s.destination_address ILIKE '%' || sqlc.narg(destination)::text || '%')
  AND (sqlc.narg(query)::text IS NULL
       OR s.tracking_number ILIKE '%' || sqlc.narg(query)::text || '%'
       OR s.customer_name ILIKE '%' || sqlc.narg(query)::text || '%'
       OR s.customer_phone ILIKE '%' || sqlc.narg(query)::text || '%'
       OR s.consignee_name ILIKE '%' || sqlc.narg(query)::text || '%'
       OR s.consignee_phone ILIKE '%' || sqlc.narg(query)::text || '%'
       OR d.full_name ILIKE '%' || sqlc.narg(query)::text || '%')
ORDER BY
  CASE WHEN sqlc.arg(sort)::text = 'created_asc' THEN s.created_at END ASC,
  CASE WHEN sqlc.arg(sort)::text = 'eta_asc' THEN s.estimated_delivery END ASC NULLS LAST,
  CASE WHEN sqlc.arg(sort)::text = 'eta_desc' THEN s.estimated_delivery END DESC NULLS LAST,
  CASE WHEN sqlc.arg(sort)::text = 'updated_desc' THEN s.updated_at END DESC,
  -- Statuses sort in the order a shipment goes through them.
  CASE WHEN sqlc.arg(sort)::text = 'status' THEN array_position(ARRAY['pending', 'assigned', 'in_transit', 'delayed', 'arrived', 'issue', 'delivered', 'cancelled'], s.status) END ASC,
  CASE WHEN sqlc.arg(sort)::text = 'status_desc' THEN array_position(ARRAY['pending', 'assigned', 'in_transit', 'delayed', 'arrived', 'issue', 'delivered', 'cancelled'], s.status) END DESC,
  CASE WHEN sqlc.arg(sort)::text = 'customer' THEN lower(s.customer_name) END ASC,
  CASE WHEN sqlc.arg(sort)::text = 'customer_desc' THEN lower(s.customer_name) END DESC,
  CASE WHEN sqlc.arg(sort)::text = 'tracking' THEN s.tracking_number END ASC,
  CASE WHEN sqlc.arg(sort)::text = 'tracking_desc' THEN s.tracking_number END DESC,
  CASE WHEN sqlc.arg(sort)::text = 'driver' THEN lower(d.full_name) END ASC NULLS LAST,
  CASE WHEN sqlc.arg(sort)::text = 'driver_desc' THEN lower(d.full_name) END DESC NULLS LAST,
  CASE WHEN sqlc.arg(sort)::text = 'route' THEN lower(COALESCE(s.origin_city, s.origin_address)) END ASC,
  CASE WHEN sqlc.arg(sort)::text = 'route' THEN lower(COALESCE(s.destination_city, s.destination_address)) END ASC,
  CASE WHEN sqlc.arg(sort)::text = 'route_desc' THEN lower(COALESCE(s.origin_city, s.origin_address)) END DESC,
  CASE WHEN sqlc.arg(sort)::text = 'route_desc' THEN lower(COALESCE(s.destination_city, s.destination_address)) END DESC,
  CASE WHEN sqlc.arg(sort)::text = 'priority' THEN
      CASE s.priority WHEN 'urgent' THEN 0 WHEN 'high' THEN 1 ELSE 2 END
  END ASC,
  s.created_at DESC
LIMIT sqlc.arg(row_limit) OFFSET sqlc.arg(row_offset);

-- name: CountShipments :one
SELECT COUNT(*)
FROM shipments s
LEFT JOIN users d ON d.id = s.driver_id
WHERE s.tenant_id = sqlc.arg(tenant_id)
  AND (cardinality(sqlc.arg(statuses)::text[]) = 0 OR s.status = ANY(sqlc.arg(statuses)::text[]))
  AND (sqlc.narg(driver_id)::uuid IS NULL OR s.driver_id = sqlc.narg(driver_id)::uuid)
  AND (sqlc.narg(vehicle_id)::uuid IS NULL OR s.vehicle_id = sqlc.narg(vehicle_id)::uuid)
  AND (sqlc.narg(priority)::text IS NULL OR s.priority = sqlc.narg(priority)::text)
  AND (sqlc.narg(date_from)::timestamptz IS NULL OR s.created_at >= sqlc.narg(date_from)::timestamptz)
  AND (sqlc.narg(date_to)::timestamptz IS NULL OR s.created_at < sqlc.narg(date_to)::timestamptz)
  AND (sqlc.narg(origin)::text IS NULL
       OR s.origin_city ILIKE sqlc.narg(origin)::text
       OR s.origin_address ILIKE '%' || sqlc.narg(origin)::text || '%')
  AND (sqlc.narg(destination)::text IS NULL
       OR s.destination_city ILIKE sqlc.narg(destination)::text
       OR s.destination_address ILIKE '%' || sqlc.narg(destination)::text || '%')
  AND (sqlc.narg(query)::text IS NULL
       OR s.tracking_number ILIKE '%' || sqlc.narg(query)::text || '%'
       OR s.customer_name ILIKE '%' || sqlc.narg(query)::text || '%'
       OR s.customer_phone ILIKE '%' || sqlc.narg(query)::text || '%'
       OR s.consignee_name ILIKE '%' || sqlc.narg(query)::text || '%'
       OR s.consignee_phone ILIKE '%' || sqlc.narg(query)::text || '%'
       OR d.full_name ILIKE '%' || sqlc.narg(query)::text || '%');

-- name: GetVehicleLoad :one
-- What a vehicle carries already: the weight of its active shipments other
-- than this one, and another driver on any of them.
SELECT COALESCE(SUM(s.cargo_weight), 0)::float8 AS weight_kg,
       COALESCE(MIN(u.full_name) FILTER (WHERE s.driver_id IS DISTINCT FROM sqlc.arg(driver_id)::uuid), '')::text AS other_driver
FROM shipments s
LEFT JOIN users u ON u.id = s.driver_id
WHERE s.tenant_id = sqlc.arg(tenant_id) AND s.vehicle_id = sqlc.arg(vehicle_id)
  AND s.status IN ('assigned', 'in_transit', 'delayed', 'arrived', 'issue')
  AND s.id IS DISTINCT FROM sqlc.narg(exclude)::uuid;

-- name: ListActiveShipmentsForMap :many
SELECT s.id, s.tracking_number, s.status, s.priority,
       s.destination_city, s.destination_lat, s.destination_lng,
       s.origin_city, s.origin_lat, s.origin_lng, s.driver_id,
       d.full_name AS driver_name, s.customer_name, s.estimated_delivery
FROM shipments s
LEFT JOIN users d ON d.id = s.driver_id
WHERE s.tenant_id = $1
  AND s.status IN ('assigned', 'in_transit', 'delayed', 'arrived', 'issue')
ORDER BY s.created_at DESC
LIMIT 500;

-- name: ListOverdueShipments :many
-- Across tenants: the job that raises ETA alerts runs once for the platform.
-- Only loads on the road can be late; one not yet collected is caught by
-- ListPickupOverdue. The episode identifies the ETA the alert is about.
-- Loads already alerted for that ETA are left out, so a backlog of more
-- than a run's worth is worked through, oldest first, over the next runs.
-- How late counts is the company's rule (alert_rules.late_minutes).
SELECT s.id, s.tenant_id, s.tracking_number, s.driver_id, s.estimated_delivery,
       floor(extract(epoch FROM s.estimated_delivery))::bigint::text AS episode
FROM shipments s
LEFT JOIN alert_rules r ON r.tenant_id = s.tenant_id
WHERE s.status IN ('in_transit', 'delayed', 'arrived', 'issue')
  AND COALESCE(r.late_on, true)
  AND s.estimated_delivery IS NOT NULL
  AND s.estimated_delivery < NOW() - make_interval(mins => COALESCE(r.late_minutes, 0))
  AND NOT EXISTS (
      SELECT 1 FROM alerts a
      WHERE a.tenant_id = s.tenant_id AND a.alert_type = 'eta_exceeded' AND a.shipment_id = s.id
        AND a.episode = floor(extract(epoch FROM s.estimated_delivery))::bigint::text)
ORDER BY s.estimated_delivery
LIMIT 1000;

-- name: ListPickupOverdue :many
-- Loads still waiting to be collected well after their planned pickup, not
-- yet alerted for that pickup time, oldest first.
SELECT s.id, s.tenant_id, s.tracking_number, s.driver_id, s.status, s.planned_pickup_at,
       floor(extract(epoch FROM s.planned_pickup_at))::bigint::text AS episode
FROM shipments s
WHERE s.status IN ('pending', 'assigned')
  AND s.planned_pickup_at IS NOT NULL
  AND s.planned_pickup_at < sqlc.arg(due_before)
  AND NOT EXISTS (
      SELECT 1 FROM alerts a
      WHERE a.tenant_id = s.tenant_id AND a.alert_type = 'pickup_overdue' AND a.shipment_id = s.id
        AND a.episode = floor(extract(epoch FROM s.planned_pickup_at))::bigint::text)
ORDER BY s.planned_pickup_at
LIMIT 1000;

-- name: ListShipmentsByCustomerPhone :many
-- A person's shipments: the ones they booked and the ones they receive.
SELECT s.tracking_number, s.status, s.origin_city, s.destination_city,
       s.estimated_delivery, s.actual_delivery, s.created_at, s.updated_at,
       t.name AS company_name
FROM shipments s
JOIN tenants t ON t.id = s.tenant_id
WHERE (s.customer_phone = sqlc.arg(phone) OR s.consignee_phone = sqlc.arg(phone))
  AND s.created_at > NOW() - INTERVAL '90 days'
ORDER BY s.created_at DESC
LIMIT 50;

-- name: ListRecentShipmentsByVehicle :many
SELECT s.*, d.full_name AS driver_name
FROM shipments s
LEFT JOIN users d ON d.id = s.driver_id
WHERE s.vehicle_id = $1 AND s.tenant_id = $2
ORDER BY s.created_at DESC
LIMIT $3;
