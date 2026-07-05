-- name: ListJobs :many
SELECT id, action, idempotency_key, actor_id, status, created_at, ttl_nanos, payload_json
FROM jobs
ORDER BY created_at ASC, id ASC;


