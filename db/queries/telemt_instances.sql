-- R-Q-03: telemt_instances — per-agent runtime metadata that the
-- panel restores at boot.

-- name: ListInstances :many
SELECT id, agent_id, name, version, config_fingerprint, connected_users, read_only, updated_at
FROM telemt_instances
ORDER BY updated_at, id;

-- name: UpsertInstance :exec
INSERT INTO telemt_instances (
    id, agent_id, name, version, config_fingerprint, connected_users, read_only, updated_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT (id) DO UPDATE
SET agent_id = EXCLUDED.agent_id,
    name = EXCLUDED.name,
    version = EXCLUDED.version,
    config_fingerprint = EXCLUDED.config_fingerprint,
    connected_users = EXCLUDED.connected_users,
    read_only = EXCLUDED.read_only,
    updated_at = EXCLUDED.updated_at;
