-- +goose Up
-- P4 final step: the seq-delta protocol is fully replaced by the
-- cumulative watermark (agent_boot_id, last_total_bytes) added in 0057
-- — drop the per-agent report cursor. Greenfield: no data migration.
ALTER TABLE client_usage DROP COLUMN last_seq;

-- +goose Down
ALTER TABLE client_usage ADD COLUMN last_seq INTEGER NOT NULL DEFAULT 0;
