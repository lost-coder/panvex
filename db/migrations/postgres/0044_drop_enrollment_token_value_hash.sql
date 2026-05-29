-- +goose Up
-- L-4: drop the dead enrollment_tokens.value_hash column. Enrollment tokens
-- are stored plaintext (TTL-bounded); the hashing this column was meant to
-- back was never implemented, leaving it a permanently-empty dead column.
-- Pre-release, no compatibility shim. DROP COLUMN cascades to the index
-- idx_enrollment_tokens_value_hash created in migration 0021.
ALTER TABLE enrollment_tokens DROP COLUMN IF EXISTS value_hash;

-- +goose Down
-- intentionally empty (pre-release, no compatibility shim)
