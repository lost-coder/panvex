-- name: ListJobTargets :many
SELECT job_id, agent_id, status, result_text, updated_at
FROM job_targets
WHERE job_id = $1
ORDER BY agent_id;

-- name: UpsertJobTarget :exec
INSERT INTO job_targets (job_id, agent_id, status, result_text, updated_at)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (job_id, agent_id) DO UPDATE
SET status = EXCLUDED.status,
    result_text = EXCLUDED.result_text,
    updated_at = EXCLUDED.updated_at;
