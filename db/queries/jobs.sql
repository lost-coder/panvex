-- name: ListJobs :many
SELECT id, action, idempotency_key, actor_id, status, created_at, ttl_nanos, payload_json
FROM jobs
ORDER BY created_at DESC;

-- name: CreateJob :exec
INSERT INTO jobs (id, action, idempotency_key, actor_id, status, created_at, ttl_nanos, payload_json)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8);
