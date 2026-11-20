-- +goose Up
-- Delivery media (docs/migration.md, phase 1, step 6; docs/audit-2026-11-19.md,
-- D6): a row per stored file of a proof of delivery, its signature or a
-- photo, with its key, size and MD5, and when and where it was taken
-- (AC-021.3, AC-021.4) where the app says so; the PWA doesn't, and
-- Kiflet/mobile's direct uploads will (phase 3).
--
-- position is 0 for the signature and 1, 2… for the photos, as the pages'
-- /media/{n} links number them. Files stored before this table have no
-- size or MD5. The proof's signature_url and photo_urls are still written
-- for a release, so that a rollback finds them, and then dropped
-- (ROADMAP); nothing reads them. signature_data has not been written
-- since the baseline.
-- +goose StatementBegin
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'proof_of_deliveries_tenant_key') THEN
        ALTER TABLE proof_of_deliveries ADD CONSTRAINT proof_of_deliveries_tenant_key UNIQUE (tenant_id, id);
    END IF;
END $$;
-- +goose StatementEnd

CREATE TABLE IF NOT EXISTS pod_media (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    tenant_id uuid NOT NULL,
    pod_id uuid NOT NULL,
    kind text NOT NULL,
    position smallint NOT NULL,
    storage_key text NOT NULL,
    content_type text NOT NULL,
    size_bytes bigint,
    md5 text,
    taken_at timestamptz,
    latitude double precision,
    longitude double precision,
    created_at timestamptz DEFAULT now() NOT NULL,
    CONSTRAINT pod_media_pkey PRIMARY KEY (id),
    CONSTRAINT pod_media_position_key UNIQUE (tenant_id, pod_id, position),
    CONSTRAINT pod_media_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE CASCADE,
    CONSTRAINT pod_media_pod_id_fkey FOREIGN KEY (tenant_id, pod_id) REFERENCES proof_of_deliveries (tenant_id, id) ON DELETE CASCADE,
    CONSTRAINT pod_media_kind_check CHECK (kind IN ('signature', 'photo') AND (kind = 'signature') = (position = 0) AND position >= 0),
    CONSTRAINT pod_media_size_check CHECK (size_bytes > 0),
    CONSTRAINT pod_media_md5_check CHECK (md5 ~ '^[0-9a-f]{32}$'),
    CONSTRAINT pod_media_place_check CHECK ((latitude IS NULL) = (longitude IS NULL))
);

INSERT INTO pod_media (tenant_id, pod_id, kind, position, storage_key, content_type, created_at)
SELECT p.tenant_id, p.id, 'signature', 0, p.signature_url, 'image/png', COALESCE(p.synced_at, p.recorded_at)
FROM proof_of_deliveries p
WHERE p.signature_url IS NOT NULL AND p.signature_url <> ''
ON CONFLICT (tenant_id, pod_id, position) DO NOTHING;

INSERT INTO pod_media (tenant_id, pod_id, kind, position, storage_key, content_type, created_at)
SELECT p.tenant_id, p.id, 'photo', u.n, u.key, 'image/jpeg', COALESCE(p.synced_at, p.recorded_at)
FROM proof_of_deliveries p
CROSS JOIN LATERAL unnest(p.photo_urls) WITH ORDINALITY AS u(key, n)
WHERE u.key <> ''
ON CONFLICT (tenant_id, pod_id, position) DO NOTHING;

-- +goose Down
DROP TABLE IF EXISTS pod_media;
ALTER TABLE proof_of_deliveries DROP CONSTRAINT IF EXISTS proof_of_deliveries_tenant_key;
