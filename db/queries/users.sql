-- R-Q-03: extend sqlc coverage to the users table. Powers user CRUD
-- and the bootstrap-admin path.

-- name: ListUsers :many
SELECT id, username, password_hash, role, totp_enabled, totp_secret, created_at
FROM users
ORDER BY created_at, id;

-- name: GetUser :one
SELECT id, username, password_hash, role, totp_enabled, totp_secret, created_at
FROM users
WHERE id = $1;

-- name: GetUserByUsername :one
SELECT id, username, password_hash, role, totp_enabled, totp_secret, created_at
FROM users
WHERE username = $1;

-- name: UpsertUser :exec
INSERT INTO users (id, username, password_hash, role, totp_enabled, totp_secret, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (id) DO UPDATE
SET username = EXCLUDED.username,
    password_hash = EXCLUDED.password_hash,
    role = EXCLUDED.role,
    totp_enabled = EXCLUDED.totp_enabled,
    totp_secret = EXCLUDED.totp_secret;

-- name: DeleteUser :execrows
DELETE FROM users WHERE id = $1;
