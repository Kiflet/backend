-- +goose Up
-- Delivery reports that came before their text was recorded as sent. A
-- gateway may report within moments of accepting a text, before the sender
-- has saved the gateway's message ID; such a report matched nothing and
-- was lost. It is kept here instead, and applied when the text is saved
-- (both sides apply after their own write commits, so whichever commits
-- second finds the other's row). Deleted after a day when no text claims
-- it. The platform's own table: a provider's callback names its message,
-- not a company.
CREATE TABLE IF NOT EXISTS sms_early_reports (
    provider text NOT NULL,
    provider_message_id text NOT NULL,
    delivery text NOT NULL,
    delivery_at timestamptz NOT NULL,
    last_error text,
    received_at timestamptz NOT NULL DEFAULT NOW(),
    PRIMARY KEY (provider, provider_message_id)
);

-- +goose Down
DROP TABLE IF EXISTS sms_early_reports;
