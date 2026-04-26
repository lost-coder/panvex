-- R-Q-03: login_lockouts — durable failure-count + lock state per
-- username so a CP restart cannot silently reset the lockout window.

-- name: GetLoginLockout :one
SELECT username, failures, locked_at, updated_at
FROM login_lockouts
WHERE username = $1;

-- name: ListLoginLockouts :many
SELECT username, failures, locked_at, updated_at
FROM login_lockouts;

-- name: UpsertLoginLockout :exec
INSERT INTO login_lockouts (username, failures, locked_at, updated_at)
VALUES ($1, $2, $3, $4)
ON CONFLICT (username) DO UPDATE SET
    failures = EXCLUDED.failures,
    locked_at = EXCLUDED.locked_at,
    updated_at = EXCLUDED.updated_at;

-- name: DeleteLoginLockout :exec
DELETE FROM login_lockouts WHERE username = $1;

-- name: DeleteExpiredLoginLockouts :execrows
DELETE FROM login_lockouts WHERE updated_at < $1;
