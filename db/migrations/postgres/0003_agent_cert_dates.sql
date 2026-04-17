-- +goose Up
ALTER TABLE agents ADD COLUMN IF NOT EXISTS cert_issued_at TIMESTAMPTZ;
ALTER TABLE agents ADD COLUMN IF NOT EXISTS cert_expires_at TIMESTAMPTZ;

-- +goose Down
ALTER TABLE agents DROP COLUMN IF EXISTS cert_expires_at;
ALTER TABLE agents DROP COLUMN IF EXISTS cert_issued_at;
