-- name: ListJobs :many
SELECT id, action, target_agent_ids, idempotency_key, actor_id, status, created_at, ttl_seconds
FROM jobs
ORDER BY created_at DESC;

-- name: CreateJob :exec
INSERT INTO jobs (id, action, target_agent_ids, idempotency_key, actor_id, status, created_at, ttl_seconds)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8);
