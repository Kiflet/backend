-- The fleet module's vehicles, with their papers (insurance, inspection).

-- name: GetVehicleByID :one
SELECT * FROM vehicles
WHERE id = $1 AND tenant_id = $2;

-- name: ListActiveVehiclesByTenant :many
SELECT * FROM vehicles
WHERE tenant_id = $1 AND is_active
ORDER BY plate_number;

-- name: CreateVehicle :one
INSERT INTO vehicles (tenant_id, plate_number, vehicle_type, capacity_kg, is_active, insurance_expires_on, inspection_due_on)
VALUES ($1, $2, $3, $4, true, sqlc.narg(insurance_expires_on), sqlc.narg(inspection_due_on))
RETURNING *;

-- name: UpdateVehicle :one
UPDATE vehicles
SET plate_number = $3,
    vehicle_type = $4,
    capacity_kg = $5,
    is_active = $6,
    insurance_expires_on = sqlc.narg(insurance_expires_on),
    inspection_due_on = sqlc.narg(inspection_due_on)
WHERE id = $1 AND tenant_id = $2
RETURNING *;

-- name: CountVehiclesByTenant :one
SELECT COUNT(*) FROM vehicles WHERE tenant_id = $1 AND is_active;
