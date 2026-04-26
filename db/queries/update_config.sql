-- R-Q-03: update_config — single-key/value table that stores update
-- channel settings + the running update-checker state.

-- name: GetUpdateConfig :one
SELECT value FROM update_config WHERE key = $1;

-- name: UpsertUpdateConfig :exec
INSERT INTO update_config (key, value)
VALUES ($1, $2)
ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value;
