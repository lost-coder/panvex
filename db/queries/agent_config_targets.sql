-- agent_config_targets — desired Telemt config per scope (group | agent).

-- name: GetAgentConfigTarget :one
SELECT scope_type, scope_id, sections_json, created_at, updated_at
FROM agent_config_targets
WHERE scope_type = $1 AND scope_id = $2;

-- name: ListAgentConfigTargets :many
SELECT scope_type, scope_id, sections_json, created_at, updated_at
FROM agent_config_targets
ORDER BY scope_type ASC, scope_id ASC;

-- name: UpsertAgentConfigTarget :exec
INSERT INTO agent_config_targets (scope_type, scope_id, sections_json, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (scope_type, scope_id) DO UPDATE
SET sections_json = EXCLUDED.sections_json,
    updated_at    = EXCLUDED.updated_at;

-- name: DeleteAgentConfigTarget :execrows
DELETE FROM agent_config_targets WHERE scope_type = $1 AND scope_id = $2;
