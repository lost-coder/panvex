-- name: ListAgents :many
SELECT id, node_name, environment_id, fleet_group_id, version, read_only, last_seen_at, created_at
FROM agents
ORDER BY last_seen_at DESC;

-- name: UpsertAgent :exec
INSERT INTO agents (id, node_name, environment_id, fleet_group_id, version, read_only, last_seen_at)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (id) DO UPDATE
SET node_name = EXCLUDED.node_name,
    environment_id = EXCLUDED.environment_id,
    fleet_group_id = EXCLUDED.fleet_group_id,
    version = EXCLUDED.version,
    read_only = EXCLUDED.read_only,
    last_seen_at = EXCLUDED.last_seen_at;
