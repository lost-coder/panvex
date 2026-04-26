-- R-Q-03: consumed_totp — replay-protection log for verified TOTP codes.

-- name: UpsertConsumedTotp :exec
INSERT INTO consumed_totp (user_id, code, used_at)
VALUES ($1, $2, $3)
ON CONFLICT (user_id, code) DO UPDATE SET used_at = EXCLUDED.used_at;

-- name: ListConsumedTotp :many
SELECT user_id, code, used_at FROM consumed_totp;

-- name: DeleteExpiredConsumedTotp :exec
DELETE FROM consumed_totp WHERE used_at < $1;
