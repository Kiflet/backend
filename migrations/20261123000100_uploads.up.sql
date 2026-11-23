-- +goose Up
-- Uploads (docs/migration.md, phase 3, M5): a file a driver's app is about
-- to put in storage, by a presigned URL, before the delivery that uses it
-- is sent. The proof module owns it. A file is known by its content: the
-- same driver asking again for the same file gets the same row and key.
-- stored_at is set when the file reached storage through the API; a
-- presigned PUT goes straight to storage, which checks its MD5.
CREATE TABLE IF NOT EXISTS uploads (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id uuid NOT NULL,
    driver_id uuid NOT NULL,
    kind text NOT NULL,
    storage_key text NOT NULL,
    content_type text NOT NULL,
    size_bytes bigint NOT NULL,
    md5 text NOT NULL,
    created_at timestamptz DEFAULT now() NOT NULL,
    stored_at timestamptz,
    CONSTRAINT uploads_pkey PRIMARY KEY (id),
    CONSTRAINT uploads_tenant_key UNIQUE (tenant_id, id),
    CONSTRAINT uploads_content_key UNIQUE (tenant_id, driver_id, kind, md5, size_bytes),
    CONSTRAINT uploads_storage_key_key UNIQUE (storage_key),
    CONSTRAINT uploads_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE CASCADE,
    CONSTRAINT uploads_driver_id_fkey FOREIGN KEY (tenant_id, driver_id) REFERENCES users (tenant_id, id) ON DELETE CASCADE,
    CONSTRAINT uploads_kind_check CHECK (kind IN ('signature', 'photo')),
    CONSTRAINT uploads_size_check CHECK (size_bytes > 0),
    CONSTRAINT uploads_md5_check CHECK (md5 ~ '^[0-9a-f]{32}$'),
    CONSTRAINT uploads_key_check CHECK (storage_key LIKE tenant_id::text || '/%')
);

-- +goose Down
DROP TABLE IF EXISTS uploads;
