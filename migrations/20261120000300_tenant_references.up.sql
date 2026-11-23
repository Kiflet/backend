-- +goose Up
-- The tenant in every reference between companies' tables (docs/migration.md,
-- phase 1, step 2; docs/audit-2026-11-19.md, D2). A row could point at
-- another company's row as long as the code never wrote one; now the
-- database refuses it. Each parent is unique on (tenant_id, id), and each
-- reference is (tenant_id, x_id) REFERENCES x (tenant_id, id), keeping its
-- name and what a delete does. SET NULL names its column, so the row's
-- company is never cleared. Rows whose tenant_id is null (the platform's
-- own texts, invoices and payments) are not checked (MATCH SIMPLE).
--
-- Four references stay on the ID alone, because the platform's operators,
-- who belong to no customer company, make them: audit_logs.user_id (what
-- an operator did to a company), invoices.created_by, payments.recorded_by
-- and support_messages.author_id (an operator's answer).
--
-- broadcast_recipients gains the tenant, from its broadcast, so that both
-- its references carry it.
ALTER TABLE broadcast_recipients ADD COLUMN IF NOT EXISTS tenant_id uuid;
UPDATE broadcast_recipients r SET tenant_id = b.tenant_id FROM broadcasts b WHERE b.id = r.broadcast_id;
ALTER TABLE broadcast_recipients ALTER COLUMN tenant_id SET NOT NULL;

-- Run again (the app migrates on start), it builds the same: the references
-- first come off, then the parents' keys are made again, then the
-- references go back on.
ALTER TABLE alert_rules DROP CONSTRAINT IF EXISTS alert_rules_updated_by_fkey;
ALTER TABLE alerts DROP CONSTRAINT IF EXISTS alerts_acknowledged_by_fkey;
ALTER TABLE alerts DROP CONSTRAINT IF EXISTS alerts_driver_id_fkey;
ALTER TABLE alerts DROP CONSTRAINT IF EXISTS alerts_shipment_id_fkey;
ALTER TABLE alerts DROP CONSTRAINT IF EXISTS alerts_vehicle_id_fkey;
ALTER TABLE api_keys DROP CONSTRAINT IF EXISTS api_keys_created_by_fkey;
ALTER TABLE broadcasts DROP CONSTRAINT IF EXISTS broadcasts_sent_by_fkey;
ALTER TABLE broadcast_recipients DROP CONSTRAINT IF EXISTS broadcast_recipients_broadcast_id_fkey;
ALTER TABLE broadcast_recipients DROP CONSTRAINT IF EXISTS broadcast_recipients_driver_id_fkey;
ALTER TABLE driver_feed DROP CONSTRAINT IF EXISTS driver_feed_driver_id_fkey;
ALTER TABLE driver_feed_heads DROP CONSTRAINT IF EXISTS driver_feed_heads_driver_id_fkey;
ALTER TABLE driver_positions DROP CONSTRAINT IF EXISTS driver_positions_driver_id_fkey;
ALTER TABLE email_tokens DROP CONSTRAINT IF EXISTS email_tokens_user_id_fkey;
ALTER TABLE payments DROP CONSTRAINT IF EXISTS payments_invoice_id_fkey;
ALTER TABLE proof_of_deliveries DROP CONSTRAINT IF EXISTS proof_of_deliveries_driver_id_fkey;
ALTER TABLE proof_of_deliveries DROP CONSTRAINT IF EXISTS proof_of_deliveries_reviewed_by_fkey;
ALTER TABLE proof_of_deliveries DROP CONSTRAINT IF EXISTS proof_of_deliveries_shipment_id_fkey;
ALTER TABLE saved_filters DROP CONSTRAINT IF EXISTS saved_filters_user_id_fkey;
ALTER TABLE sessions DROP CONSTRAINT IF EXISTS sessions_user_id_fkey;
ALTER TABLE shipment_charges DROP CONSTRAINT IF EXISTS shipment_charges_created_by_fkey;
ALTER TABLE shipment_charges DROP CONSTRAINT IF EXISTS shipment_charges_shipment_id_fkey;
ALTER TABLE shipment_documents DROP CONSTRAINT IF EXISTS shipment_documents_shipment_id_fkey;
ALTER TABLE shipment_documents DROP CONSTRAINT IF EXISTS shipment_documents_uploaded_by_fkey;
ALTER TABLE shipment_events DROP CONSTRAINT IF EXISTS shipment_events_actor_id_fkey;
ALTER TABLE shipment_events DROP CONSTRAINT IF EXISTS shipment_events_driver_id_fkey;
ALTER TABLE shipment_events DROP CONSTRAINT IF EXISTS shipment_events_previous_driver_id_fkey;
ALTER TABLE shipment_events DROP CONSTRAINT IF EXISTS shipment_events_shipment_id_fkey;
ALTER TABLE shipments DROP CONSTRAINT IF EXISTS shipments_created_by_fkey;
ALTER TABLE shipments DROP CONSTRAINT IF EXISTS shipments_driver_id_fkey;
ALTER TABLE shipments DROP CONSTRAINT IF EXISTS shipments_vehicle_id_fkey;
ALTER TABLE sms_messages DROP CONSTRAINT IF EXISTS sms_messages_shipment_id_fkey;
ALTER TABLE support_tickets DROP CONSTRAINT IF EXISTS support_tickets_opened_by_fkey;
ALTER TABLE support_messages DROP CONSTRAINT IF EXISTS support_messages_ticket_id_fkey;
ALTER TABLE tracking_events DROP CONSTRAINT IF EXISTS tracking_events_driver_id_fkey;
ALTER TABLE tracking_events DROP CONSTRAINT IF EXISTS tracking_events_shipment_id_fkey;
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_vehicle_id_fkey;
ALTER TABLE webhook_deliveries DROP CONSTRAINT IF EXISTS webhook_deliveries_endpoint_id_fkey;
ALTER TABLE webhook_deliveries DROP CONSTRAINT IF EXISTS webhook_deliveries_shipment_id_fkey;
ALTER TABLE webhook_endpoints DROP CONSTRAINT IF EXISTS webhook_endpoints_created_by_fkey;

-- Each key is added only where it is missing, so that a later
-- migration's reference to one doesn't stop this one running again.
-- +goose StatementBegin
DO $$
DECLARE
    k record;
BEGIN
    FOR k IN SELECT * FROM (VALUES
        ('users', 'users_tenant_key', 'tenant_id, id'),
        ('vehicles', 'vehicles_tenant_key', 'tenant_id, id'),
        ('shipments', 'shipments_tenant_key', 'tenant_id, id'),
        ('invoices', 'invoices_tenant_key', 'tenant_id, id'),
        ('support_tickets', 'support_tickets_tenant_key', 'tenant_id, id'),
        ('webhook_endpoints', 'webhook_endpoints_tenant_key', 'tenant_id, id'),
        ('broadcasts', 'broadcasts_tenant_key', 'tenant_id, id'),
        ('driver_feed_heads', 'driver_feed_heads_tenant_key', 'tenant_id, driver_id')
    ) AS v(tbl, name, cols) LOOP
        IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = k.name) THEN
            EXECUTE format('ALTER TABLE %I ADD CONSTRAINT %I UNIQUE (%s)', k.tbl, k.name, k.cols);
        END IF;
    END LOOP;
END $$;
-- +goose StatementEnd

ALTER TABLE alert_rules ADD CONSTRAINT alert_rules_updated_by_fkey FOREIGN KEY (tenant_id, updated_by) REFERENCES users (tenant_id, id) ON DELETE SET NULL (updated_by);
ALTER TABLE alerts ADD CONSTRAINT alerts_acknowledged_by_fkey FOREIGN KEY (tenant_id, acknowledged_by) REFERENCES users (tenant_id, id) ON DELETE SET NULL (acknowledged_by);
ALTER TABLE alerts ADD CONSTRAINT alerts_driver_id_fkey FOREIGN KEY (tenant_id, driver_id) REFERENCES users (tenant_id, id) ON DELETE SET NULL (driver_id);
ALTER TABLE alerts ADD CONSTRAINT alerts_shipment_id_fkey FOREIGN KEY (tenant_id, shipment_id) REFERENCES shipments (tenant_id, id) ON DELETE CASCADE;
ALTER TABLE alerts ADD CONSTRAINT alerts_vehicle_id_fkey FOREIGN KEY (tenant_id, vehicle_id) REFERENCES vehicles (tenant_id, id) ON DELETE CASCADE;
ALTER TABLE api_keys ADD CONSTRAINT api_keys_created_by_fkey FOREIGN KEY (tenant_id, created_by) REFERENCES users (tenant_id, id) ON DELETE SET NULL (created_by);
ALTER TABLE broadcasts ADD CONSTRAINT broadcasts_sent_by_fkey FOREIGN KEY (tenant_id, sent_by) REFERENCES users (tenant_id, id) ON DELETE SET NULL (sent_by);
ALTER TABLE broadcast_recipients ADD CONSTRAINT broadcast_recipients_broadcast_id_fkey FOREIGN KEY (tenant_id, broadcast_id) REFERENCES broadcasts (tenant_id, id) ON DELETE CASCADE;
ALTER TABLE broadcast_recipients ADD CONSTRAINT broadcast_recipients_driver_id_fkey FOREIGN KEY (tenant_id, driver_id) REFERENCES users (tenant_id, id) ON DELETE CASCADE;
ALTER TABLE driver_feed ADD CONSTRAINT driver_feed_driver_id_fkey FOREIGN KEY (tenant_id, driver_id) REFERENCES driver_feed_heads (tenant_id, driver_id) ON DELETE CASCADE;
ALTER TABLE driver_feed_heads ADD CONSTRAINT driver_feed_heads_driver_id_fkey FOREIGN KEY (tenant_id, driver_id) REFERENCES users (tenant_id, id) ON DELETE CASCADE;
ALTER TABLE driver_positions ADD CONSTRAINT driver_positions_driver_id_fkey FOREIGN KEY (tenant_id, driver_id) REFERENCES users (tenant_id, id) ON DELETE CASCADE;
ALTER TABLE email_tokens ADD CONSTRAINT email_tokens_user_id_fkey FOREIGN KEY (tenant_id, user_id) REFERENCES users (tenant_id, id) ON DELETE CASCADE;
ALTER TABLE payments ADD CONSTRAINT payments_invoice_id_fkey FOREIGN KEY (tenant_id, invoice_id) REFERENCES invoices (tenant_id, id);
ALTER TABLE proof_of_deliveries ADD CONSTRAINT proof_of_deliveries_driver_id_fkey FOREIGN KEY (tenant_id, driver_id) REFERENCES users (tenant_id, id);
ALTER TABLE proof_of_deliveries ADD CONSTRAINT proof_of_deliveries_reviewed_by_fkey FOREIGN KEY (tenant_id, reviewed_by) REFERENCES users (tenant_id, id) ON DELETE SET NULL (reviewed_by);
ALTER TABLE proof_of_deliveries ADD CONSTRAINT proof_of_deliveries_shipment_id_fkey FOREIGN KEY (tenant_id, shipment_id) REFERENCES shipments (tenant_id, id) ON DELETE CASCADE;
ALTER TABLE saved_filters ADD CONSTRAINT saved_filters_user_id_fkey FOREIGN KEY (tenant_id, user_id) REFERENCES users (tenant_id, id) ON DELETE CASCADE;
ALTER TABLE sessions ADD CONSTRAINT sessions_user_id_fkey FOREIGN KEY (tenant_id, user_id) REFERENCES users (tenant_id, id) ON DELETE CASCADE;
ALTER TABLE shipment_charges ADD CONSTRAINT shipment_charges_created_by_fkey FOREIGN KEY (tenant_id, created_by) REFERENCES users (tenant_id, id) ON DELETE SET NULL (created_by);
ALTER TABLE shipment_charges ADD CONSTRAINT shipment_charges_shipment_id_fkey FOREIGN KEY (tenant_id, shipment_id) REFERENCES shipments (tenant_id, id) ON DELETE CASCADE;
ALTER TABLE shipment_documents ADD CONSTRAINT shipment_documents_shipment_id_fkey FOREIGN KEY (tenant_id, shipment_id) REFERENCES shipments (tenant_id, id) ON DELETE CASCADE;
ALTER TABLE shipment_documents ADD CONSTRAINT shipment_documents_uploaded_by_fkey FOREIGN KEY (tenant_id, uploaded_by) REFERENCES users (tenant_id, id) ON DELETE SET NULL (uploaded_by);
ALTER TABLE shipment_events ADD CONSTRAINT shipment_events_actor_id_fkey FOREIGN KEY (tenant_id, actor_id) REFERENCES users (tenant_id, id) ON DELETE SET NULL (actor_id);
ALTER TABLE shipment_events ADD CONSTRAINT shipment_events_driver_id_fkey FOREIGN KEY (tenant_id, driver_id) REFERENCES users (tenant_id, id) ON DELETE SET NULL (driver_id);
ALTER TABLE shipment_events ADD CONSTRAINT shipment_events_previous_driver_id_fkey FOREIGN KEY (tenant_id, previous_driver_id) REFERENCES users (tenant_id, id) ON DELETE SET NULL (previous_driver_id);
ALTER TABLE shipment_events ADD CONSTRAINT shipment_events_shipment_id_fkey FOREIGN KEY (tenant_id, shipment_id) REFERENCES shipments (tenant_id, id) ON DELETE CASCADE;
ALTER TABLE shipments ADD CONSTRAINT shipments_created_by_fkey FOREIGN KEY (tenant_id, created_by) REFERENCES users (tenant_id, id);
ALTER TABLE shipments ADD CONSTRAINT shipments_driver_id_fkey FOREIGN KEY (tenant_id, driver_id) REFERENCES users (tenant_id, id) ON DELETE SET NULL (driver_id);
ALTER TABLE shipments ADD CONSTRAINT shipments_vehicle_id_fkey FOREIGN KEY (tenant_id, vehicle_id) REFERENCES vehicles (tenant_id, id) ON DELETE SET NULL (vehicle_id);
ALTER TABLE sms_messages ADD CONSTRAINT sms_messages_shipment_id_fkey FOREIGN KEY (tenant_id, shipment_id) REFERENCES shipments (tenant_id, id) ON DELETE SET NULL (shipment_id);
ALTER TABLE support_tickets ADD CONSTRAINT support_tickets_opened_by_fkey FOREIGN KEY (tenant_id, opened_by) REFERENCES users (tenant_id, id) ON DELETE SET NULL (opened_by);
ALTER TABLE support_messages ADD CONSTRAINT support_messages_ticket_id_fkey FOREIGN KEY (tenant_id, ticket_id) REFERENCES support_tickets (tenant_id, id) ON DELETE CASCADE;
ALTER TABLE tracking_events ADD CONSTRAINT tracking_events_driver_id_fkey FOREIGN KEY (tenant_id, driver_id) REFERENCES users (tenant_id, id) ON DELETE SET NULL (driver_id);
ALTER TABLE tracking_events ADD CONSTRAINT tracking_events_shipment_id_fkey FOREIGN KEY (tenant_id, shipment_id) REFERENCES shipments (tenant_id, id) ON DELETE CASCADE;
ALTER TABLE users ADD CONSTRAINT users_vehicle_id_fkey FOREIGN KEY (tenant_id, vehicle_id) REFERENCES vehicles (tenant_id, id) ON DELETE SET NULL (vehicle_id);
ALTER TABLE webhook_deliveries ADD CONSTRAINT webhook_deliveries_endpoint_id_fkey FOREIGN KEY (tenant_id, endpoint_id) REFERENCES webhook_endpoints (tenant_id, id) ON DELETE CASCADE;
ALTER TABLE webhook_deliveries ADD CONSTRAINT webhook_deliveries_shipment_id_fkey FOREIGN KEY (tenant_id, shipment_id) REFERENCES shipments (tenant_id, id) ON DELETE SET NULL (shipment_id);
ALTER TABLE webhook_endpoints ADD CONSTRAINT webhook_endpoints_created_by_fkey FOREIGN KEY (tenant_id, created_by) REFERENCES users (tenant_id, id) ON DELETE SET NULL (created_by);

-- +goose Down
ALTER TABLE webhook_endpoints DROP CONSTRAINT IF EXISTS webhook_endpoints_created_by_fkey;
ALTER TABLE webhook_deliveries DROP CONSTRAINT IF EXISTS webhook_deliveries_shipment_id_fkey;
ALTER TABLE webhook_deliveries DROP CONSTRAINT IF EXISTS webhook_deliveries_endpoint_id_fkey;
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_vehicle_id_fkey;
ALTER TABLE tracking_events DROP CONSTRAINT IF EXISTS tracking_events_shipment_id_fkey;
ALTER TABLE tracking_events DROP CONSTRAINT IF EXISTS tracking_events_driver_id_fkey;
ALTER TABLE support_messages DROP CONSTRAINT IF EXISTS support_messages_ticket_id_fkey;
ALTER TABLE support_tickets DROP CONSTRAINT IF EXISTS support_tickets_opened_by_fkey;
ALTER TABLE sms_messages DROP CONSTRAINT IF EXISTS sms_messages_shipment_id_fkey;
ALTER TABLE shipments DROP CONSTRAINT IF EXISTS shipments_vehicle_id_fkey;
ALTER TABLE shipments DROP CONSTRAINT IF EXISTS shipments_driver_id_fkey;
ALTER TABLE shipments DROP CONSTRAINT IF EXISTS shipments_created_by_fkey;
ALTER TABLE shipment_events DROP CONSTRAINT IF EXISTS shipment_events_shipment_id_fkey;
ALTER TABLE shipment_events DROP CONSTRAINT IF EXISTS shipment_events_previous_driver_id_fkey;
ALTER TABLE shipment_events DROP CONSTRAINT IF EXISTS shipment_events_driver_id_fkey;
ALTER TABLE shipment_events DROP CONSTRAINT IF EXISTS shipment_events_actor_id_fkey;
ALTER TABLE shipment_documents DROP CONSTRAINT IF EXISTS shipment_documents_uploaded_by_fkey;
ALTER TABLE shipment_documents DROP CONSTRAINT IF EXISTS shipment_documents_shipment_id_fkey;
ALTER TABLE shipment_charges DROP CONSTRAINT IF EXISTS shipment_charges_shipment_id_fkey;
ALTER TABLE shipment_charges DROP CONSTRAINT IF EXISTS shipment_charges_created_by_fkey;
ALTER TABLE sessions DROP CONSTRAINT IF EXISTS sessions_user_id_fkey;
ALTER TABLE saved_filters DROP CONSTRAINT IF EXISTS saved_filters_user_id_fkey;
ALTER TABLE proof_of_deliveries DROP CONSTRAINT IF EXISTS proof_of_deliveries_shipment_id_fkey;
ALTER TABLE proof_of_deliveries DROP CONSTRAINT IF EXISTS proof_of_deliveries_reviewed_by_fkey;
ALTER TABLE proof_of_deliveries DROP CONSTRAINT IF EXISTS proof_of_deliveries_driver_id_fkey;
ALTER TABLE payments DROP CONSTRAINT IF EXISTS payments_invoice_id_fkey;
ALTER TABLE email_tokens DROP CONSTRAINT IF EXISTS email_tokens_user_id_fkey;
ALTER TABLE driver_positions DROP CONSTRAINT IF EXISTS driver_positions_driver_id_fkey;
ALTER TABLE driver_feed_heads DROP CONSTRAINT IF EXISTS driver_feed_heads_driver_id_fkey;
ALTER TABLE driver_feed DROP CONSTRAINT IF EXISTS driver_feed_driver_id_fkey;
ALTER TABLE broadcast_recipients DROP CONSTRAINT IF EXISTS broadcast_recipients_driver_id_fkey;
ALTER TABLE broadcast_recipients DROP CONSTRAINT IF EXISTS broadcast_recipients_broadcast_id_fkey;
ALTER TABLE broadcasts DROP CONSTRAINT IF EXISTS broadcasts_sent_by_fkey;
ALTER TABLE api_keys DROP CONSTRAINT IF EXISTS api_keys_created_by_fkey;
ALTER TABLE alerts DROP CONSTRAINT IF EXISTS alerts_vehicle_id_fkey;
ALTER TABLE alerts DROP CONSTRAINT IF EXISTS alerts_shipment_id_fkey;
ALTER TABLE alerts DROP CONSTRAINT IF EXISTS alerts_driver_id_fkey;
ALTER TABLE alerts DROP CONSTRAINT IF EXISTS alerts_acknowledged_by_fkey;
ALTER TABLE alert_rules DROP CONSTRAINT IF EXISTS alert_rules_updated_by_fkey;
ALTER TABLE driver_feed_heads DROP CONSTRAINT IF EXISTS driver_feed_heads_tenant_key;
ALTER TABLE broadcasts DROP CONSTRAINT IF EXISTS broadcasts_tenant_key;
ALTER TABLE webhook_endpoints DROP CONSTRAINT IF EXISTS webhook_endpoints_tenant_key;
ALTER TABLE support_tickets DROP CONSTRAINT IF EXISTS support_tickets_tenant_key;
ALTER TABLE invoices DROP CONSTRAINT IF EXISTS invoices_tenant_key;
ALTER TABLE shipments DROP CONSTRAINT IF EXISTS shipments_tenant_key;
ALTER TABLE vehicles DROP CONSTRAINT IF EXISTS vehicles_tenant_key;
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_tenant_key;
ALTER TABLE webhook_endpoints ADD CONSTRAINT webhook_endpoints_created_by_fkey FOREIGN KEY (created_by) REFERENCES users (id) ON DELETE SET NULL;
ALTER TABLE webhook_deliveries ADD CONSTRAINT webhook_deliveries_shipment_id_fkey FOREIGN KEY (shipment_id) REFERENCES shipments (id) ON DELETE SET NULL;
ALTER TABLE webhook_deliveries ADD CONSTRAINT webhook_deliveries_endpoint_id_fkey FOREIGN KEY (endpoint_id) REFERENCES webhook_endpoints (id) ON DELETE CASCADE;
ALTER TABLE users ADD CONSTRAINT users_vehicle_id_fkey FOREIGN KEY (vehicle_id) REFERENCES vehicles (id) ON DELETE SET NULL;
ALTER TABLE tracking_events ADD CONSTRAINT tracking_events_shipment_id_fkey FOREIGN KEY (shipment_id) REFERENCES shipments (id) ON DELETE CASCADE;
ALTER TABLE tracking_events ADD CONSTRAINT tracking_events_driver_id_fkey FOREIGN KEY (driver_id) REFERENCES users (id) ON DELETE SET NULL;
ALTER TABLE support_messages ADD CONSTRAINT support_messages_ticket_id_fkey FOREIGN KEY (ticket_id) REFERENCES support_tickets (id) ON DELETE CASCADE;
ALTER TABLE support_tickets ADD CONSTRAINT support_tickets_opened_by_fkey FOREIGN KEY (opened_by) REFERENCES users (id) ON DELETE SET NULL;
ALTER TABLE sms_messages ADD CONSTRAINT sms_messages_shipment_id_fkey FOREIGN KEY (shipment_id) REFERENCES shipments (id) ON DELETE SET NULL;
ALTER TABLE shipments ADD CONSTRAINT shipments_vehicle_id_fkey FOREIGN KEY (vehicle_id) REFERENCES vehicles (id) ON DELETE SET NULL;
ALTER TABLE shipments ADD CONSTRAINT shipments_driver_id_fkey FOREIGN KEY (driver_id) REFERENCES users (id) ON DELETE SET NULL;
ALTER TABLE shipments ADD CONSTRAINT shipments_created_by_fkey FOREIGN KEY (created_by) REFERENCES users (id);
ALTER TABLE shipment_events ADD CONSTRAINT shipment_events_shipment_id_fkey FOREIGN KEY (shipment_id) REFERENCES shipments (id) ON DELETE CASCADE;
ALTER TABLE shipment_events ADD CONSTRAINT shipment_events_previous_driver_id_fkey FOREIGN KEY (previous_driver_id) REFERENCES users (id) ON DELETE SET NULL;
ALTER TABLE shipment_events ADD CONSTRAINT shipment_events_driver_id_fkey FOREIGN KEY (driver_id) REFERENCES users (id) ON DELETE SET NULL;
ALTER TABLE shipment_events ADD CONSTRAINT shipment_events_actor_id_fkey FOREIGN KEY (actor_id) REFERENCES users (id) ON DELETE SET NULL;
ALTER TABLE shipment_documents ADD CONSTRAINT shipment_documents_uploaded_by_fkey FOREIGN KEY (uploaded_by) REFERENCES users (id) ON DELETE SET NULL;
ALTER TABLE shipment_documents ADD CONSTRAINT shipment_documents_shipment_id_fkey FOREIGN KEY (shipment_id) REFERENCES shipments (id) ON DELETE CASCADE;
ALTER TABLE shipment_charges ADD CONSTRAINT shipment_charges_shipment_id_fkey FOREIGN KEY (shipment_id) REFERENCES shipments (id) ON DELETE CASCADE;
ALTER TABLE shipment_charges ADD CONSTRAINT shipment_charges_created_by_fkey FOREIGN KEY (created_by) REFERENCES users (id) ON DELETE SET NULL;
ALTER TABLE sessions ADD CONSTRAINT sessions_user_id_fkey FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE;
ALTER TABLE saved_filters ADD CONSTRAINT saved_filters_user_id_fkey FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE;
ALTER TABLE proof_of_deliveries ADD CONSTRAINT proof_of_deliveries_shipment_id_fkey FOREIGN KEY (shipment_id) REFERENCES shipments (id) ON DELETE CASCADE;
ALTER TABLE proof_of_deliveries ADD CONSTRAINT proof_of_deliveries_reviewed_by_fkey FOREIGN KEY (reviewed_by) REFERENCES users (id) ON DELETE SET NULL;
ALTER TABLE proof_of_deliveries ADD CONSTRAINT proof_of_deliveries_driver_id_fkey FOREIGN KEY (driver_id) REFERENCES users (id);
ALTER TABLE payments ADD CONSTRAINT payments_invoice_id_fkey FOREIGN KEY (invoice_id) REFERENCES invoices (id);
ALTER TABLE email_tokens ADD CONSTRAINT email_tokens_user_id_fkey FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE;
ALTER TABLE driver_positions ADD CONSTRAINT driver_positions_driver_id_fkey FOREIGN KEY (driver_id) REFERENCES users (id) ON DELETE CASCADE;
ALTER TABLE driver_feed_heads ADD CONSTRAINT driver_feed_heads_driver_id_fkey FOREIGN KEY (driver_id) REFERENCES users (id) ON DELETE CASCADE;
ALTER TABLE driver_feed ADD CONSTRAINT driver_feed_driver_id_fkey FOREIGN KEY (driver_id) REFERENCES driver_feed_heads (driver_id) ON DELETE CASCADE;
ALTER TABLE broadcast_recipients ADD CONSTRAINT broadcast_recipients_driver_id_fkey FOREIGN KEY (driver_id) REFERENCES users (id) ON DELETE CASCADE;
ALTER TABLE broadcast_recipients ADD CONSTRAINT broadcast_recipients_broadcast_id_fkey FOREIGN KEY (broadcast_id) REFERENCES broadcasts (id) ON DELETE CASCADE;
ALTER TABLE broadcasts ADD CONSTRAINT broadcasts_sent_by_fkey FOREIGN KEY (sent_by) REFERENCES users (id) ON DELETE SET NULL;
ALTER TABLE api_keys ADD CONSTRAINT api_keys_created_by_fkey FOREIGN KEY (created_by) REFERENCES users (id) ON DELETE SET NULL;
ALTER TABLE alerts ADD CONSTRAINT alerts_vehicle_id_fkey FOREIGN KEY (vehicle_id) REFERENCES vehicles (id) ON DELETE CASCADE;
ALTER TABLE alerts ADD CONSTRAINT alerts_shipment_id_fkey FOREIGN KEY (shipment_id) REFERENCES shipments (id) ON DELETE CASCADE;
ALTER TABLE alerts ADD CONSTRAINT alerts_driver_id_fkey FOREIGN KEY (driver_id) REFERENCES users (id) ON DELETE SET NULL;
ALTER TABLE alerts ADD CONSTRAINT alerts_acknowledged_by_fkey FOREIGN KEY (acknowledged_by) REFERENCES users (id) ON DELETE SET NULL;
ALTER TABLE alert_rules ADD CONSTRAINT alert_rules_updated_by_fkey FOREIGN KEY (updated_by) REFERENCES users (id) ON DELETE SET NULL;
ALTER TABLE broadcast_recipients DROP COLUMN IF EXISTS tenant_id;
