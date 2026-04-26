-- +goose Up
ALTER TABLE enrollment_tokens ADD COLUMN IF NOT EXISTS value_hash TEXT NOT NULL DEFAULT '';
-- +goose StatementBegin
-- +goose NO TRANSACTION
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_enrollment_tokens_value_hash ON enrollment_tokens(value_hash);
-- +goose StatementEnd

-- +goose Down
-- +goose NO TRANSACTION
DROP INDEX CONCURRENTLY IF EXISTS idx_enrollment_tokens_value_hash;
ALTER TABLE enrollment_tokens DROP COLUMN IF EXISTS value_hash;
