-- 0001_create_webhook_tables.sql
-- Creates the webhook integration tables in the plugin schema.
-- Run with search_path = plugin_data_com_paca_webhook, public.

CREATE TABLE IF NOT EXISTS webhooks (
    id           UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id   UUID        NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    url          TEXT        NOT NULL,
    secret_enc   TEXT        NOT NULL DEFAULT '',
    events       JSONB       NOT NULL DEFAULT '[]',
    enabled      BOOLEAN     NOT NULL DEFAULT TRUE,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_webhooks_project_id
    ON webhooks (project_id);

CREATE TABLE IF NOT EXISTS webhook_deliveries (
    id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    webhook_id  UUID        NOT NULL REFERENCES webhooks(id) ON DELETE CASCADE,
    event_type  TEXT        NOT NULL,
    status_code INT         NOT NULL DEFAULT 0,
    success     BOOLEAN     NOT NULL DEFAULT FALSE,
    error       TEXT        NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_webhook_deliveries_webhook_id
    ON webhook_deliveries (webhook_id, created_at DESC);
