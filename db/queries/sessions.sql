-- Q5.U-Q-03: sqlc widening — sessions table joins the agents/jobs/
-- job_targets cluster the postgres backend already routes through
-- dbsqlc. Bringing more tables under sqlc gives the postgres-side
-- code path compile-time type safety on every column.

-- name: GetSession :one
SELECT id, user_id, created_at, last_seen_at
FROM sessions
WHERE id = $1;

-- name: ListSessions :many
SELECT id, user_id, created_at, last_seen_at
FROM sessions
ORDER BY created_at DESC;

-- name: DeleteExpiredSessions :exec
DELETE FROM sessions WHERE created_at < $1;

-- name: DeleteSession :execrows
DELETE FROM sessions WHERE id = $1;
