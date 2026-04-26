-- +goose Up
ALTER TABLE enrollment_tokens ADD COLUMN value_hash TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_enrollment_tokens_value_hash ON enrollment_tokens(value_hash);

-- +goose Down
DROP INDEX IF EXISTS idx_enrollment_tokens_value_hash;
-- SQLite < 3.35 cannot DROP COLUMN inline; rebuild minimal copy.
CREATE TABLE enrollment_tokens_old AS SELECT * FROM enrollment_tokens;
DROP TABLE enrollment_tokens;
ALTER TABLE enrollment_tokens_old RENAME TO enrollment_tokens;
