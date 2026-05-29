-- +goose Up
-- L-4: drop the dead enrollment_tokens.value_hash column (see the Postgres
-- 0044 mirror). The column is indexed, and SQLite cannot DROP an indexed
-- column inline, so rebuild the table without it (same recipe as 0021's
-- Down) and recreate the surviving fleet_group_id index. Pre-release, no
-- compatibility shim.
DROP INDEX IF EXISTS idx_enrollment_tokens_value_hash;
CREATE TABLE enrollment_tokens_new (
    value TEXT PRIMARY KEY,
    fleet_group_id TEXT,
    issued_at_unix INTEGER NOT NULL,
    expires_at_unix INTEGER NOT NULL,
    consumed_at_unix INTEGER,
    revoked_at_unix INTEGER
);
INSERT INTO enrollment_tokens_new (value, fleet_group_id, issued_at_unix, expires_at_unix, consumed_at_unix, revoked_at_unix)
    SELECT value, fleet_group_id, issued_at_unix, expires_at_unix, consumed_at_unix, revoked_at_unix
    FROM enrollment_tokens;
DROP TABLE enrollment_tokens;
ALTER TABLE enrollment_tokens_new RENAME TO enrollment_tokens;
CREATE INDEX idx_enrollment_tokens_fleet_group_id ON enrollment_tokens (fleet_group_id);

-- +goose Down
-- intentionally empty (pre-release, no compatibility shim)
