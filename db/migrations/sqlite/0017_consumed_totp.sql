-- +goose Up
CREATE TABLE IF NOT EXISTS consumed_totp (
    user_id TEXT NOT NULL,
    code TEXT NOT NULL,
    used_at_unix INTEGER NOT NULL,
    PRIMARY KEY (user_id, code)
);
CREATE INDEX IF NOT EXISTS idx_consumed_totp_used_at ON consumed_totp(used_at_unix);

-- +goose Down
DROP INDEX IF EXISTS idx_consumed_totp_used_at;
DROP TABLE IF EXISTS consumed_totp;
