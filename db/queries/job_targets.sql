-- name: ListJobTargets :many
SELECT job_id, agent_id, status, result_text, result_json, updated_at
FROM job_targets
WHERE job_id = $1
ORDER BY agent_id;

