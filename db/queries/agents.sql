-- name: ListAgents :many
SELECT id, node_name, fleet_group_id, version, read_only,
       last_seen_at, cert_issued_at, cert_expires_at
FROM agents
ORDER BY last_seen_at ASC, id ASC;


-- name: UpsertAgent :exec
INSERT INTO agents (id, node_name, fleet_group_id, version, read_only,
                    last_seen_at, cert_issued_at, cert_expires_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT (id) DO UPDATE
SET node_name       = EXCLUDED.node_name,
    fleet_group_id  = EXCLUDED.fleet_group_id,
    version         = EXCLUDED.version,
    read_only       = EXCLUDED.read_only,
    last_seen_at    = EXCLUDED.last_seen_at,
    cert_issued_at  = EXCLUDED.cert_issued_at,
    cert_expires_at = EXCLUDED.cert_expires_at;
