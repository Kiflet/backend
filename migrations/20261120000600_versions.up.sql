-- +goose Up
-- Versions (docs/migration.md, phase 1, step 7; docs/audit-2026-11-19.md,
-- D7): what a person edits carries a version, raised by every update that
-- changes it, so that phase 2's If-Match can refuse an edit made from a
-- stale copy instead of overwriting someone else's.
--
-- The trigger raises it, so no query can forget to, and no query can set
-- it. Columns the system keeps for itself (updated_at, sign-in counters,
-- the SMS billing month) and credentials, which no client reads, are left
-- out: a dispatcher's edit isn't refused because a driver signed in or
-- changed their PIN meanwhile.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION kiflet_bump_version() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    -- TG_ARGV: the columns that don't count as an edit.
    IF (to_jsonb(NEW) - TG_ARGV - 'version') IS DISTINCT FROM (to_jsonb(OLD) - TG_ARGV - 'version') THEN
        NEW.version := OLD.version + 1;
    ELSE
        NEW.version := OLD.version;
    END IF;
    RETURN NEW;
END $$;
-- +goose StatementEnd

ALTER TABLE shipments ADD COLUMN IF NOT EXISTS version integer NOT NULL DEFAULT 1;
ALTER TABLE users ADD COLUMN IF NOT EXISTS version integer NOT NULL DEFAULT 1;
ALTER TABLE vehicles ADD COLUMN IF NOT EXISTS version integer NOT NULL DEFAULT 1;
ALTER TABLE tenants ADD COLUMN IF NOT EXISTS version integer NOT NULL DEFAULT 1;
ALTER TABLE webhook_endpoints ADD COLUMN IF NOT EXISTS version integer NOT NULL DEFAULT 1;
ALTER TABLE alert_rules ADD COLUMN IF NOT EXISTS version integer NOT NULL DEFAULT 1;

DROP TRIGGER IF EXISTS shipments_version ON shipments;
CREATE TRIGGER shipments_version BEFORE UPDATE ON shipments FOR EACH ROW
    EXECUTE FUNCTION kiflet_bump_version('updated_at', 'confirm_token');
DROP TRIGGER IF EXISTS users_version ON users;
CREATE TRIGGER users_version BEFORE UPDATE ON users FOR EACH ROW
    EXECUTE FUNCTION kiflet_bump_version('updated_at', 'failed_login_attempts', 'locked_until', 'last_login_at', 'alerts_seen_at',
        'password_hash', 'pin_hash', 'password_reset_required');
DROP TRIGGER IF EXISTS vehicles_version ON vehicles;
CREATE TRIGGER vehicles_version BEFORE UPDATE ON vehicles FOR EACH ROW
    EXECUTE FUNCTION kiflet_bump_version('updated_at');
DROP TRIGGER IF EXISTS tenants_version ON tenants;
CREATE TRIGGER tenants_version BEFORE UPDATE ON tenants FOR EACH ROW
    EXECUTE FUNCTION kiflet_bump_version('updated_at', 'sms_billed_month', 'sms_month_quota');
DROP TRIGGER IF EXISTS webhook_endpoints_version ON webhook_endpoints;
CREATE TRIGGER webhook_endpoints_version BEFORE UPDATE ON webhook_endpoints FOR EACH ROW
    EXECUTE FUNCTION kiflet_bump_version('updated_at');
DROP TRIGGER IF EXISTS alert_rules_version ON alert_rules;
CREATE TRIGGER alert_rules_version BEFORE UPDATE ON alert_rules FOR EACH ROW
    EXECUTE FUNCTION kiflet_bump_version('updated_at');

-- +goose Down
DROP TRIGGER IF EXISTS shipments_version ON shipments;
DROP TRIGGER IF EXISTS users_version ON users;
DROP TRIGGER IF EXISTS vehicles_version ON vehicles;
DROP TRIGGER IF EXISTS tenants_version ON tenants;
DROP TRIGGER IF EXISTS webhook_endpoints_version ON webhook_endpoints;
DROP TRIGGER IF EXISTS alert_rules_version ON alert_rules;
DROP FUNCTION IF EXISTS kiflet_bump_version();
ALTER TABLE shipments DROP COLUMN IF EXISTS version;
ALTER TABLE users DROP COLUMN IF EXISTS version;
ALTER TABLE vehicles DROP COLUMN IF EXISTS version;
ALTER TABLE tenants DROP COLUMN IF EXISTS version;
ALTER TABLE webhook_endpoints DROP COLUMN IF EXISTS version;
ALTER TABLE alert_rules DROP COLUMN IF EXISTS version;
