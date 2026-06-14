-- R-Q-03: sqlc coverage for enrollment_tokens. The dead value_hash
-- column (added in migration 0021, never read or written) was dropped in
-- migration 0044 — enrollment tokens are stored plaintext, TTL-bounded.

-- name: GetEnrollmentToken :one
SELECT value, fleet_group_id, issued_at, expires_at, consumed_at, revoked_at
FROM enrollment_tokens
WHERE value = $1;

-- name: ListEnrollmentTokens :many
SELECT value, fleet_group_id, issued_at, expires_at, consumed_at, revoked_at
FROM enrollment_tokens
ORDER BY issued_at ASC, value ASC;


-- name: UpsertEnrollmentToken :exec
INSERT INTO enrollment_tokens (value, fleet_group_id, issued_at, expires_at, consumed_at, revoked_at)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (value) DO UPDATE
SET fleet_group_id = EXCLUDED.fleet_group_id,
    issued_at = EXCLUDED.issued_at,
    expires_at = EXCLUDED.expires_at,
    consumed_at = EXCLUDED.consumed_at,
    revoked_at = EXCLUDED.revoked_at;

-- name: ConsumeEnrollmentToken :execrows
UPDATE enrollment_tokens
SET consumed_at = $1
WHERE value = $2 AND consumed_at IS NULL AND revoked_at IS NULL;

-- name: RevokeEnrollmentToken :execrows
UPDATE enrollment_tokens
SET revoked_at = $1
WHERE value = $2 AND consumed_at IS NULL AND revoked_at IS NULL;

-- name: PruneEnrollmentTokens :execrows
DELETE FROM enrollment_tokens
WHERE (consumed_at IS NOT NULL AND consumed_at < $1)
   OR (revoked_at IS NOT NULL AND revoked_at < $1)
   OR (expires_at < $1 AND consumed_at IS NULL AND revoked_at IS NULL);
