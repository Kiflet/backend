-- +goose Up
-- The driver sync's change feed (docs/architecture.md, section 11, "The
-- driver sync"; docs/audit-2026-11-19.md, D1). Every change a driver's
-- phone must hear of writes an entry, in the transaction of the change,
-- numbered in that driver's own sequence; a sync reads the entries after
-- its cursor. The number comes from the driver's head, raised with an
-- upsert that keeps its row locked until the transaction commits, so a
-- driver's entries become visible in the order of their numbers and a
-- reader that sees entry n sees every entry before it.
--
-- pruned_through is the last number the retention job has deleted: a
-- cursor before it has missed entries and gets a full download.
CREATE TABLE IF NOT EXISTS driver_feed_heads (
    driver_id uuid PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    tenant_id uuid NOT NULL REFERENCES tenants (id) ON DELETE CASCADE,
    seq bigint NOT NULL DEFAULT 0,
    pruned_through bigint NOT NULL DEFAULT 0
);

-- kind: a load (shipment), a note from the office about one (message, the
-- shipment_events row) or a message to every driver (broadcast). op:
-- upsert, or remove for a load taken from the driver, with the reason.
CREATE TABLE IF NOT EXISTS driver_feed (
    driver_id uuid NOT NULL REFERENCES driver_feed_heads (driver_id) ON DELETE CASCADE,
    seq bigint NOT NULL,
    tenant_id uuid NOT NULL REFERENCES tenants (id) ON DELETE CASCADE,
    kind text NOT NULL CHECK (kind IN ('shipment', 'message', 'broadcast')),
    ref_id uuid NOT NULL,
    op text NOT NULL CHECK (op IN ('upsert', 'remove')),
    reason text CHECK (reason IN ('reassigned', 'unassigned', 'cancelled')),
    created_at timestamptz NOT NULL DEFAULT NOW(),
    PRIMARY KEY (driver_id, seq),
    CHECK ((op = 'remove') = (reason IS NOT NULL))
);

-- For the retention job.
CREATE INDEX IF NOT EXISTS driver_feed_created_at ON driver_feed (created_at);

-- +goose Down
DROP TABLE IF EXISTS driver_feed;
DROP TABLE IF EXISTS driver_feed_heads;
