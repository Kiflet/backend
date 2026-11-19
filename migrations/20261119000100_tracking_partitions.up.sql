-- +goose Up
-- GPS positions partitioned by day (docs/architecture.md, section 5), the
-- one table that grows fast: ~6 million rows a day at 20,000 drivers.
-- Retention drops a whole day's partition instead of deleting its rows,
-- which is instant and leaves nothing for vacuum.
--
-- Partitions are UTC days named tracking_events_pYYYYMMDD. The periodic
-- jobs make a week of them ahead (tracking_events_ensure_partitions). A
-- position outside every partition (a phone whose clock is years out, or a
-- day that has no partition yet) lands in tracking_events_default, and is
-- moved into its day's partition when that partition is made.
--
-- A partitioned table's unique keys must include the partition key, so a
-- position is now one per (device_id, client_id, recorded_at). A phone
-- that sends a position again sends it with the same time, so retries
-- still change nothing.
--
-- The rows are copied inside this migration: before the first customer the
-- table is small. A large table would be copied in the background instead
-- (docs/migration.md, section 6).

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION tracking_events_ensure_partitions(first_day date, last_day date) RETURNS integer
    LANGUAGE plpgsql
    AS $$
DECLARE
    d date;
    name text;
    lo timestamptz;
    hi timestamptz;
    made integer := 0;
BEGIN
    d := first_day;
    WHILE d <= last_day LOOP
        name := 'tracking_events_p' || to_char(d, 'YYYYMMDD');
        IF to_regclass(name) IS NULL THEN
            lo := d::timestamp AT TIME ZONE 'UTC';
            hi := (d + 1)::timestamp AT TIME ZONE 'UTC';
            -- Made apart, filled from the default partition, then attached:
            -- attaching a range the default partition holds rows of fails.
            EXECUTE format('CREATE TABLE %I (LIKE tracking_events INCLUDING DEFAULTS INCLUDING CONSTRAINTS)', name);
            EXECUTE format('WITH moved AS (DELETE FROM tracking_events_default WHERE recorded_at >= %L AND recorded_at < %L RETURNING *) '
                           'INSERT INTO %I SELECT * FROM moved', lo, hi, name);
            EXECUTE format('ALTER TABLE tracking_events ATTACH PARTITION %I FOR VALUES FROM (%L) TO (%L)', name, lo, hi);
            made := made + 1;
        END IF;
        d := d + 1;
    END LOOP;
    RETURN made;
END;
$$;
-- +goose StatementEnd

-- What the periodic jobs run: partitions for the next days_ahead days,
-- and for any recent day whose positions are waiting in the default
-- partition (a phone sending a day it kept offline, from before its
-- partition existed). Returns how many it made.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION tracking_events_maintain(days_ahead integer, keep interval) RETURNS integer
    LANGUAGE plpgsql
    AS $$
DECLARE
    today date := (now() AT TIME ZONE 'UTC')::date;
    d date;
    made integer;
BEGIN
    made := tracking_events_ensure_partitions(today, today + days_ahead);
    FOR d IN
        SELECT DISTINCT (recorded_at AT TIME ZONE 'UTC')::date
        FROM tracking_events_default
        WHERE recorded_at >= now() - keep AND recorded_at < now() + make_interval(days => days_ahead + 1)
    LOOP
        made := made + tracking_events_ensure_partitions(d, d);
    END LOOP;
    RETURN made;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION tracking_events_drop_partitions(before timestamptz) RETURNS integer
    LANGUAGE plpgsql
    AS $$
DECLARE
    part record;
    dropped integer := 0;
BEGIN
    FOR part IN
        SELECT c.relname
        FROM pg_inherits i
        JOIN pg_class c ON c.oid = i.inhrelid
        WHERE i.inhparent = 'tracking_events'::regclass
          AND c.relname ~ '^tracking_events_p[0-9]{8}$'
          -- The day's end is at or before the cut-off: all of it is old.
          AND ((to_date(substring(c.relname from 18), 'YYYYMMDD') + 1)::timestamp AT TIME ZONE 'UTC') <= before
    LOOP
        EXECUTE format('DROP TABLE %I', part.relname);
        dropped := dropped + 1;
    END LOOP;
    DELETE FROM tracking_events_default WHERE recorded_at < before;
    RETURN dropped;
END;
$$;
-- +goose StatementEnd

-- The conversion, once: run again on a partitioned table, it does nothing.
-- +goose StatementBegin
DO $$
BEGIN
    IF (SELECT relkind FROM pg_class WHERE oid = 'tracking_events'::regclass) = 'p' THEN
        RETURN;
    END IF;

    -- The table as it was, out of the way. Its indexes go now, as their names
    -- are wanted for the new table's.
    ALTER TABLE tracking_events RENAME TO tracking_events_unpartitioned;
    ALTER TABLE tracking_events_unpartitioned RENAME CONSTRAINT tracking_events_pkey TO tracking_events_unpartitioned_pkey;
    DROP INDEX IF EXISTS idx_tracking_coords;
    DROP INDEX IF EXISTS idx_tracking_driver;
    DROP INDEX IF EXISTS idx_tracking_driver_recorded;
    DROP INDEX IF EXISTS idx_tracking_events_device_client;
    DROP INDEX IF EXISTS idx_tracking_recorded;
    DROP INDEX IF EXISTS idx_tracking_shipment_recorded;

    CREATE TABLE tracking_events (
        id uuid DEFAULT gen_random_uuid() NOT NULL,
        tenant_id uuid NOT NULL,
        shipment_id uuid NOT NULL,
        driver_id uuid,
        coordinates geometry(Point,4326) NOT NULL,
        accuracy_meters numeric(10,2),
        speed_kph numeric(10,2),
        heading numeric(5,2),
        event_type text NOT NULL,
        status text,
        note text,
        recorded_at timestamptz NOT NULL,
        synced_at timestamptz,
        device_id text,
        battery_level integer,
        client_id text,
        CONSTRAINT tracking_events_pkey PRIMARY KEY (id, recorded_at),
        CONSTRAINT tracking_events_driver_id_fkey FOREIGN KEY (driver_id) REFERENCES users(id) ON DELETE SET NULL,
        CONSTRAINT tracking_events_shipment_id_fkey FOREIGN KEY (shipment_id) REFERENCES shipments(id) ON DELETE CASCADE,
        CONSTRAINT tracking_events_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE CASCADE,
        CONSTRAINT tracking_events_event_type_check CHECK (event_type IN ('gps_ping', 'checkpoint', 'status_change'))
    ) PARTITION BY RANGE (recorded_at);

    CREATE TABLE tracking_events_default PARTITION OF tracking_events DEFAULT;

    -- On the parent, so every partition gets them. The GiST index on
    -- coordinates isn't made again: no query searches positions by place (the
    -- map's distance checks cast to geography, which it couldn't serve).
    CREATE INDEX idx_tracking_driver ON tracking_events (driver_id);
    CREATE INDEX idx_tracking_driver_recorded ON tracking_events (tenant_id, driver_id, recorded_at DESC);
    CREATE INDEX idx_tracking_shipment_recorded ON tracking_events (shipment_id, recorded_at);
    CREATE INDEX idx_tracking_recorded ON tracking_events USING brin (recorded_at);
    CREATE UNIQUE INDEX idx_tracking_events_device_client ON tracking_events (device_id, client_id, recorded_at)
        WHERE client_id IS NOT NULL AND device_id IS NOT NULL;

    INSERT INTO tracking_events (
        id, tenant_id, shipment_id, driver_id, coordinates, accuracy_meters, speed_kph, heading,
        event_type, status, note, recorded_at, synced_at, device_id, battery_level, client_id
    )
    SELECT id, tenant_id, shipment_id, driver_id, coordinates, accuracy_meters, speed_kph, heading,
           event_type, status, note, recorded_at, synced_at, device_id, battery_level, client_id
    FROM tracking_events_unpartitioned;

    DROP TABLE tracking_events_unpartitioned;
END;
$$;
-- +goose StatementEnd

-- The copied rows are in the default partition: give each of the 90 days
-- retention keeps its partition, and make the week ahead. Older rows stay
-- in the default partition until the next retention run.
SELECT tracking_events_maintain(7, interval '90 days');

-- +goose Down
CREATE TABLE tracking_events_unpartitioned (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id uuid NOT NULL,
    shipment_id uuid NOT NULL,
    driver_id uuid,
    coordinates geometry(Point,4326) NOT NULL,
    accuracy_meters numeric(10,2),
    speed_kph numeric(10,2),
    heading numeric(5,2),
    event_type text NOT NULL,
    status text,
    note text,
    recorded_at timestamptz NOT NULL,
    synced_at timestamptz,
    device_id text,
    battery_level integer,
    client_id text,
    CONSTRAINT tracking_events_unpartitioned_pkey PRIMARY KEY (id),
    CONSTRAINT tracking_events_driver_id_fkey FOREIGN KEY (driver_id) REFERENCES users(id) ON DELETE SET NULL,
    CONSTRAINT tracking_events_shipment_id_fkey FOREIGN KEY (shipment_id) REFERENCES shipments(id) ON DELETE CASCADE,
    CONSTRAINT tracking_events_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE CASCADE,
    CONSTRAINT tracking_events_event_type_check CHECK (event_type IN ('gps_ping', 'checkpoint', 'status_change'))
);
INSERT INTO tracking_events_unpartitioned SELECT * FROM tracking_events;
DROP TABLE tracking_events;
ALTER TABLE tracking_events_unpartitioned RENAME TO tracking_events;
ALTER TABLE tracking_events RENAME CONSTRAINT tracking_events_unpartitioned_pkey TO tracking_events_pkey;
CREATE INDEX IF NOT EXISTS idx_tracking_coords ON tracking_events USING gist (coordinates);
CREATE INDEX IF NOT EXISTS idx_tracking_driver ON tracking_events (driver_id);
CREATE INDEX IF NOT EXISTS idx_tracking_driver_recorded ON tracking_events (tenant_id, driver_id, recorded_at DESC);
CREATE UNIQUE INDEX IF NOT EXISTS idx_tracking_events_device_client ON tracking_events (device_id, client_id) WHERE ((client_id IS NOT NULL) AND (device_id IS NOT NULL));
CREATE INDEX IF NOT EXISTS idx_tracking_recorded ON tracking_events (recorded_at);
CREATE INDEX IF NOT EXISTS idx_tracking_shipment_recorded ON tracking_events (shipment_id, recorded_at);
DROP FUNCTION IF EXISTS tracking_events_drop_partitions(timestamptz);
DROP FUNCTION IF EXISTS tracking_events_maintain(integer, interval);
DROP FUNCTION IF EXISTS tracking_events_ensure_partitions(date, date);
