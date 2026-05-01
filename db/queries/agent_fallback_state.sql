-- name: PutAgentFallbackState :exec
INSERT INTO agent_fallback_state (agent_id, entered_at_unix)
VALUES ($1, $2)
ON CONFLICT (agent_id) DO NOTHING;

-- name: DeleteAgentFallbackState :exec
DELETE FROM agent_fallback_state WHERE agent_id = $1;

-- name: GetAgentFallbackState :one
SELECT agent_id, entered_at_unix
FROM agent_fallback_state
WHERE agent_id = $1;

-- name: ListAgentFallbackState :many
SELECT agent_id, entered_at_unix
FROM agent_fallback_state
ORDER BY entered_at_unix ASC;
