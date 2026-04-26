-- +goose Up
CREATE TABLE IF NOT EXISTS cp_secrets (
    key TEXT PRIMARY KEY,
    value BYTEA NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- +goose Down
DROP TABLE IF EXISTS cp_secrets;
