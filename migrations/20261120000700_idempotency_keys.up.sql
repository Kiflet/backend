-- +goose Up
-- Idempotency keys (docs/migration.md, phase 1, step 8;
-- docs/audit-2026-11-19.md, D7; docs/architecture.md, section 11): a create
-- sent with an Idempotency-Key is remembered for 24 hours, by company,
-- caller and key, with the request's hash and the first response. A replay
-- gets that response; the same key with another request is refused. In
-- Postgres, not Redis, so a replay is recognised even after Redis is lost.
--
-- caller_id is the user or the API key that sent it. response_status is
-- empty while the first request runs.
CREATE TABLE IF NOT EXISTS idempotency_keys (
    tenant_id uuid NOT NULL,
    caller_id uuid NOT NULL,
    key text NOT NULL,
    request_hash bytea NOT NULL,
    response_status smallint,
    response_type text,
    response_body bytea,
    created_at timestamptz DEFAULT now() NOT NULL,
    completed_at timestamptz,
    CONSTRAINT idempotency_keys_pkey PRIMARY KEY (tenant_id, caller_id, key),
    CONSTRAINT idempotency_keys_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE CASCADE,
    CONSTRAINT idempotency_keys_key_check CHECK (length(key) BETWEEN 1 AND 255),
    CONSTRAINT idempotency_keys_hash_check CHECK (octet_length(request_hash) = 32),
    CONSTRAINT idempotency_keys_done_check CHECK ((response_status IS NULL) = (completed_at IS NULL)),
    CONSTRAINT idempotency_keys_status_check CHECK (response_status BETWEEN 100 AND 599)
);
CREATE INDEX IF NOT EXISTS idempotency_keys_created_at ON idempotency_keys (created_at);

-- +goose Down
DROP TABLE IF EXISTS idempotency_keys;
