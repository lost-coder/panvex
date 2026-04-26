-- R-Q-03: cp_secrets — control-plane key/value secret store.

-- name: GetCPSecret :one
SELECT value FROM cp_secrets WHERE key = $1;

-- name: UpsertCPSecret :exec
INSERT INTO cp_secrets (key, value, updated_at)
VALUES ($1, $2, $3)
ON CONFLICT (key) DO UPDATE SET
    value = EXCLUDED.value,
    updated_at = EXCLUDED.updated_at;
