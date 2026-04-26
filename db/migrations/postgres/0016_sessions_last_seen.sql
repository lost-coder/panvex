-- +goose Up
ALTER TABLE sessions ADD COLUMN IF NOT EXISTS last_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW();
UPDATE sessions SET last_seen_at = created_at WHERE last_seen_at < created_at;

-- +goose Down
ALTER TABLE sessions DROP COLUMN IF EXISTS last_seen_at;
