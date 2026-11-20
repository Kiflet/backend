-- +goose Up
-- The headers a replay carries too (Location, ETag), beside the first
-- response's status, type and body (docs/migration.md, phase 2, step 4).
ALTER TABLE idempotency_keys ADD COLUMN IF NOT EXISTS response_headers jsonb;

-- +goose Down
ALTER TABLE idempotency_keys DROP COLUMN IF EXISTS response_headers;
