-- +goose Up
ALTER TABLE enrollment_tokens ADD COLUMN value_hash TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_enrollment_tokens_value_hash ON enrollment_tokens(value_hash);

-- +goose Down
DROP INDEX IF EXISTS idx_enrollment_tokens_value_hash;
-- SQLite < 3.35 cannot DROP COLUMN inline; rebuild a copy without value_hash.
CREATE TABLE enrollment_tokens_old (
    value TEXT PRIMARY KEY,
    fleet_group_id TEXT,
    issued_at_unix INTEGER NOT NULL,
    expires_at_unix INTEGER NOT NULL,
    consumed_at_unix INTEGER,
    revoked_at_unix INTEGER
);
INSERT INTO enrollment_tokens_old (value, fleet_group_id, issued_at_unix, expires_at_unix, consumed_at_unix, revoked_at_unix)
    SELECT value, fleet_group_id, issued_at_unix, expires_at_unix, consumed_at_unix, revoked_at_unix
    FROM enrollment_tokens;
DROP TABLE enrollment_tokens;
ALTER TABLE enrollment_tokens_old RENAME TO enrollment_tokens;
