-- The shipments module's shipments.

-- name: GetShipmentByID :one
SELECT * FROM shipments
WHERE id = $1 AND tenant_id = $2;

-- name: GetShipmentByIDForUpdate :one
-- Row lock for the read-modify-write of a status transition, so two
-- concurrent updates (a driver's sync and a dispatcher's click) serialise
-- instead of both validating against the same stale status.
SELECT * FROM shipments
WHERE id = $1 AND tenant_id = $2
FOR UPDATE;

-- name: GetShipmentByTrackingNumber :one
SELECT * FROM shipments
WHERE tracking_number = $1;

-- name: GetShipmentByConfirmToken :one
SELECT * FROM shipments
WHERE confirm_token = $1;

-- name: CreateShipment :one
INSERT INTO shipments (
    tenant_id, tracking_number, priority,
    origin_city, origin_address, origin_lat, origin_lng,
    destination_city, destination_address, destination_lat, destination_lng, destination_precise,
    customer_name, customer_phone,
    cargo_description, cargo_weight, cargo_value_santim, special_instructions,
    driver_id, vehicle_id, status, estimated_delivery, eta_manual, created_by, status_changed_at,
    planned_pickup_at, consignee_name, consignee_phone, cargo_pieces, cargo_unit
) VALUES (
    $1, $2, $3,
    $4, $5, $6, $7,
    $8, $9, $10, $11, $12,
    $13, $14,
    $15, $16, $17, $18,
    $19, $20, $21, $22, $23, $24, NOW(),
    sqlc.narg(planned_pickup_at), sqlc.narg(consignee_name), sqlc.narg(consignee_phone),
    sqlc.narg(cargo_pieces), sqlc.narg(cargo_unit)
) RETURNING *;

-- name: UpdateShipmentDetails :one
-- Edits replace every editable field; the form always submits all of them.
UPDATE shipments
SET priority = $3,
    origin_city = $4,
    origin_address = $5,
    origin_lat = $6,
    origin_lng = $7,
    destination_city = $8,
    destination_address = $9,
    destination_lat = $10,
    destination_lng = $11,
    destination_precise = $12,
    customer_name = $13,
    customer_phone = $14,
    cargo_description = $15,
    cargo_weight = $16,
    cargo_value_santim = $17,
    special_instructions = $18,
    estimated_delivery = $19,
    eta_manual = $20,
    planned_pickup_at = sqlc.narg(planned_pickup_at),
    consignee_name = sqlc.narg(consignee_name),
    consignee_phone = sqlc.narg(consignee_phone),
    cargo_pieces = sqlc.narg(cargo_pieces),
    cargo_unit = sqlc.narg(cargo_unit),
    updated_at = NOW()
WHERE id = $1 AND tenant_id = $2
RETURNING *;

-- name: SetShipmentETA :exec
-- Recalculated estimates never overwrite one a dispatcher entered by hand.
UPDATE shipments SET estimated_delivery = $3
WHERE id = $1 AND tenant_id = $2 AND NOT eta_manual;

-- name: SetShipmentAssignment :one
UPDATE shipments
SET driver_id = $3,
    vehicle_id = $4,
    status = $5,
    status_changed_at = CASE WHEN status <> $5 THEN NOW() ELSE status_changed_at END,
    updated_at = NOW()
WHERE id = $1 AND tenant_id = $2
RETURNING *;

-- name: SetShipmentDriver :one
-- Credits a delivery to the driver who made it, when the office accepts a
-- proof recorded after the shipment was handed to someone else.
UPDATE shipments
SET driver_id = sqlc.arg(driver_id), updated_at = NOW()
WHERE id = sqlc.arg(id) AND tenant_id = sqlc.arg(tenant_id)
RETURNING *;

-- name: SetShipmentStatusDetail :one
-- A new cause for the status a shipment is already in, such as a second
-- delay. When the status was entered is unchanged.
UPDATE shipments
SET status_reason = sqlc.narg(status_reason), status_note = sqlc.narg(status_note), updated_at = NOW()
WHERE id = sqlc.arg(id) AND tenant_id = sqlc.arg(tenant_id)
RETURNING *;

-- name: SetShipmentStatus :one
-- actual_delivery is stamped the first time a shipment is delivered and kept
-- thereafter; picked_up_at likewise records the first departure. Both use the
-- time the change happened, which for an offline driver is earlier than now.
-- A delivery accepted on review is stamped with when the proof was recorded
-- (delivered_at), not when the office accepted it. A shipment delivered
-- without being marked departed (a proof of delivery straight from
-- assigned) left then too, as far as anyone knows.
UPDATE shipments
SET status = sqlc.arg(status),
    status_note = sqlc.narg(status_note),
    status_reason = sqlc.narg(status_reason),
    status_changed_at = sqlc.arg(changed_at),
    actual_delivery = CASE
        WHEN sqlc.arg(status) = 'delivered' THEN COALESCE(actual_delivery, sqlc.narg(delivered_at), sqlc.arg(changed_at))
        ELSE actual_delivery
    END,
    picked_up_at = CASE
        WHEN sqlc.arg(status) IN ('in_transit', 'arrived') THEN COALESCE(picked_up_at, sqlc.arg(changed_at))
        WHEN sqlc.arg(status) = 'delivered' THEN COALESCE(picked_up_at, sqlc.narg(delivered_at), sqlc.arg(changed_at))
        ELSE picked_up_at
    END,
    updated_at = NOW()
WHERE id = sqlc.arg(id) AND tenant_id = sqlc.arg(tenant_id)
RETURNING *;

-- name: SetShipmentConfirmToken :exec
UPDATE shipments SET confirm_token = sqlc.arg(confirm_token)
WHERE id = sqlc.arg(id) AND tenant_id = sqlc.arg(tenant_id) AND confirm_token IS NULL;

-- name: SetCustomerConfirmation :one
UPDATE shipments
SET customer_confirmed_at = COALESCE(customer_confirmed_at, NOW()),
    customer_feedback = COALESCE(sqlc.narg(feedback), customer_feedback),
    updated_at = NOW()
WHERE id = sqlc.arg(id) AND tenant_id = sqlc.arg(tenant_id)
RETURNING *;

-- name: ListActiveShipmentsByDriver :many
SELECT * FROM shipments
WHERE driver_id = $1
  AND tenant_id = $2
  AND status IN ('assigned', 'in_transit', 'delayed', 'arrived', 'issue')
ORDER BY CASE priority WHEN 'urgent' THEN 0 WHEN 'high' THEN 1 ELSE 2 END,
         estimated_delivery ASC NULLS LAST,
         created_at ASC;

-- name: CountActiveShipmentsByDriver :one
SELECT COUNT(*) FROM shipments
WHERE driver_id = $1
  AND tenant_id = $2
  AND status IN ('assigned', 'in_transit', 'delayed', 'arrived', 'issue');

-- name: ListShipmentsForDriverSync :many
-- Everything the driver's device should hold, for a full download: active
-- work, plus recently finished work so the completed list survives a
-- reinstall. Later changes reach the device through the driver feed.
SELECT * FROM shipments
WHERE driver_id = sqlc.arg(driver_id)
  AND tenant_id = sqlc.arg(tenant_id)
  AND (
    status IN ('assigned', 'in_transit', 'delayed', 'arrived', 'issue')
    OR updated_at > NOW() - INTERVAL '7 days'
  )
ORDER BY created_at DESC
LIMIT 200;

-- name: ListShipmentsByIDs :many
-- The loads a driver's feed names, as they are now.
SELECT * FROM shipments
WHERE tenant_id = sqlc.arg(tenant_id) AND id = ANY(sqlc.arg(ids)::uuid[]);

-- name: CountShipmentsByStatus :many
SELECT status, COUNT(*)::bigint AS total
FROM shipments
WHERE tenant_id = $1
GROUP BY status;

-- name: CountDeliveredSince :one
SELECT COUNT(*) FROM shipments
WHERE tenant_id = $1 AND status = 'delivered' AND actual_delivery >= $2;

-- name: CountShipmentsCreatedSince :one
SELECT COUNT(*) FROM shipments
WHERE tenant_id = $1 AND created_at >= $2;

-- name: ListRecentShipmentsByDriver :many
SELECT * FROM shipments
WHERE driver_id = $1 AND tenant_id = $2
ORDER BY created_at DESC
LIMIT $3;

-- name: RouteTransitHours :many
-- Hours from departure to delivery for recent deliveries between two cities.
-- ETA estimation takes the median once enough history exists.
SELECT (EXTRACT(EPOCH FROM (actual_delivery - COALESCE(picked_up_at, created_at))) / 3600.0)::float8 AS hours
FROM shipments
WHERE tenant_id = $1
  AND status = 'delivered'
  AND origin_city = $2
  AND destination_city = $3
  AND actual_delivery IS NOT NULL
  AND actual_delivery > COALESCE(picked_up_at, created_at)
ORDER BY actual_delivery DESC
LIMIT 50;

-- name: SetCustomerFeedback :one
-- A problem report: the customer has answered but not confirmed receipt.
UPDATE shipments
SET customer_feedback = sqlc.arg(feedback), updated_at = NOW()
WHERE id = sqlc.arg(id) AND tenant_id = sqlc.arg(tenant_id)
RETURNING *;
