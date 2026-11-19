-- Every company with activity figures from other modules' tables (users,
-- shipments, invoices), for the platform's companies page: a read model,
-- which moves to reports with that module. The tenants table itself is the
-- tenancy module's (internal/modules/tenancy).

-- name: ListTenantsWithStats :many
SELECT t.*,
       (SELECT COUNT(*) FROM users u WHERE u.tenant_id = t.id AND u.role = 'driver' AND u.is_active)::bigint AS drivers,
       (SELECT COUNT(*) FROM users u WHERE u.tenant_id = t.id AND u.role <> 'driver' AND u.is_active)::bigint AS staff,
       (SELECT COUNT(*) FROM shipments s WHERE s.tenant_id = t.id AND s.created_at > NOW() - INTERVAL '30 days')::bigint AS shipments_30d,
       (SELECT MAX(s.created_at) FROM shipments s WHERE s.tenant_id = t.id)::timestamptz AS last_shipment_at,
       (SELECT MIN(i.due_at) FROM invoices i
        WHERE i.tenant_id = t.id AND i.status = 'open' AND i.kind IN ('renewal', 'usage'))::timestamptz AS debt_due_at
FROM tenants t
ORDER BY t.created_at DESC;
