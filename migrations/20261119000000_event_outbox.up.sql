-- +goose Up
-- The event outbox (docs/architecture.md, section 6). A change that others
-- must hear about writes its events here in the same transaction, so no
-- event is lost and none is sent for a change that was rolled back. The
-- relay (cmd/relay, or the relay inside `server serve`) reads new rows,
-- publishes each to RabbitMQ with a confirm, and marks it sent.
--
-- It has no foreign key to tenants: an event about a company being deleted
-- must outlive the company. tenant_id is NULL for the platform's own events.
CREATE TABLE IF NOT EXISTS event_outbox (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid,
    -- What happened, e.g. shipment.status_changed; the consumers' contract.
    type text NOT NULL CHECK (type ~ '^[a-z_]+(\.[a-z_]+)+$'),
    version integer NOT NULL DEFAULT 1 CHECK (version > 0),
    -- Where it goes: an exchange of docs/architecture.md section 6 and its
    -- routing key.
    exchange text NOT NULL CHECK (exchange IN ('kiflet.events', 'kiflet.commands', 'kiflet.live', 'kiflet.jobs')),
    routing_key text NOT NULL,
    payload jsonb NOT NULL,
    trace_id text,
    created_at timestamptz NOT NULL DEFAULT now(),
    -- Not before: a publish that failed waits a while before the next try.
    available_at timestamptz NOT NULL DEFAULT now(),
    sent_at timestamptz,
    attempts integer NOT NULL DEFAULT 0,
    last_error text
);

-- The relay's work: what is waiting, oldest first.
CREATE INDEX IF NOT EXISTS idx_event_outbox_waiting ON event_outbox (available_at, created_at) WHERE sent_at IS NULL;
-- Retention: sent rows are deleted after a few days.
CREATE INDEX IF NOT EXISTS idx_event_outbox_sent ON event_outbox (sent_at) WHERE sent_at IS NOT NULL;

-- Wakes the relay at once, instead of at its next poll. One notification a
-- statement, however many rows it wrote.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION event_outbox_notify() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    PERFORM pg_notify('event_outbox', '');
    RETURN NULL;
END;
$$;
-- +goose StatementEnd

DROP TRIGGER IF EXISTS event_outbox_notify ON event_outbox;
CREATE TRIGGER event_outbox_notify AFTER INSERT ON event_outbox
    FOR EACH STATEMENT EXECUTE FUNCTION event_outbox_notify();

-- What each consumer has already handled. RabbitMQ delivers at least once,
-- so a consumer records a message's ID in the same transaction as its
-- effect, and skips a message it has seen. Kept seven days.
CREATE TABLE IF NOT EXISTS processed_messages (
    consumer text NOT NULL,
    message_id uuid NOT NULL,
    processed_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (consumer, message_id)
);
CREATE INDEX IF NOT EXISTS idx_processed_messages_at ON processed_messages (processed_at);

-- +goose Down
DROP TABLE IF EXISTS processed_messages;
DROP TRIGGER IF EXISTS event_outbox_notify ON event_outbox;
DROP FUNCTION IF EXISTS event_outbox_notify();
DROP TABLE IF EXISTS event_outbox;
