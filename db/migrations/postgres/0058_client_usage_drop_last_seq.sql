-- +goose Up
-- P4: см. sqlite/0058_client_usage_drop_last_seq.sql.
ALTER TABLE client_usage DROP COLUMN IF EXISTS last_seq;

-- +goose Down
ALTER TABLE client_usage ADD COLUMN IF NOT EXISTS last_seq BIGINT NOT NULL DEFAULT 0;
