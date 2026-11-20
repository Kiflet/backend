-- +goose Up
-- Money in santim (docs/migration.md, phase 1, step 4; docs/audit-2026-11-19.md,
-- D4): a load's cargo value and its charges in whole santim, as billing
-- is, instead of birr with two decimals. The new columns are added beside
-- the old, which the code no longer reads or writes; the old go a release
-- later (ROADMAP), so that a rollback finds them. Until then the old
-- charge amount may be empty.
--
-- A cargo value of 0 is none, as it was.
ALTER TABLE shipments ADD COLUMN IF NOT EXISTS cargo_value_santim bigint NOT NULL DEFAULT 0;
ALTER TABLE shipment_charges ADD COLUMN IF NOT EXISTS amount_santim bigint;
UPDATE shipments SET cargo_value_santim = round(cargo_value * 100)
WHERE cargo_value IS NOT NULL AND cargo_value_santim = 0;
UPDATE shipment_charges SET amount_santim = round(amount * 100) WHERE amount_santim IS NULL;
ALTER TABLE shipment_charges ALTER COLUMN amount_santim SET NOT NULL;
ALTER TABLE shipment_charges ALTER COLUMN amount DROP NOT NULL;

ALTER TABLE shipments DROP CONSTRAINT IF EXISTS shipments_cargo_value_santim_check;
ALTER TABLE shipments ADD CONSTRAINT shipments_cargo_value_santim_check
    CHECK (cargo_value_santim >= 0 AND cargo_value_santim < 100000000000000);
-- A charge is never nothing; a discount is negative.
ALTER TABLE shipment_charges DROP CONSTRAINT IF EXISTS shipment_charges_amount_santim_check;
ALTER TABLE shipment_charges ADD CONSTRAINT shipment_charges_amount_santim_check
    CHECK (amount_santim <> 0 AND abs(amount_santim) < 100000000000000);
ALTER TABLE shipment_charges DROP CONSTRAINT IF EXISTS shipment_charges_amount_check;

-- +goose Down
ALTER TABLE shipment_charges ADD CONSTRAINT shipment_charges_amount_check CHECK (amount <> 0 AND abs(amount) < 1000000000000);
UPDATE shipment_charges SET amount = amount_santim / 100.0 WHERE amount IS NULL;
ALTER TABLE shipment_charges ALTER COLUMN amount SET NOT NULL;
UPDATE shipments SET cargo_value = cargo_value_santim / 100.0 WHERE cargo_value IS NULL AND cargo_value_santim > 0;
ALTER TABLE shipment_charges DROP COLUMN IF EXISTS amount_santim;
ALTER TABLE shipments DROP COLUMN IF EXISTS cargo_value_santim;
