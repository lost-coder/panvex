-- +goose Up
-- P4: см. sqlite/0057_client_usage_watermark.sql (watermark кумулятивного
-- счётчика агента; additive — last_seq уходит в 0058).
ALTER TABLE client_usage ADD COLUMN IF NOT EXISTS agent_boot_id TEXT NOT NULL DEFAULT '';
ALTER TABLE client_usage ADD COLUMN IF NOT EXISTS last_total_bytes BIGINT NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE client_usage DROP COLUMN IF EXISTS last_total_bytes;
ALTER TABLE client_usage DROP COLUMN IF EXISTS agent_boot_id;
