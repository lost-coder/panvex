-- +goose Up
ALTER TABLE agents ADD COLUMN IF NOT EXISTS cert_serial TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE agents DROP COLUMN IF EXISTS cert_serial;
