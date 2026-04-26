-- R-Q-03: discovered_clients — agent-reported user records pending
-- adoption.

-- name: GetDiscoveredClient :one
SELECT id, agent_id, client_name, secret, status, total_octets,
       current_connections, active_unique_ips, connection_link,
       max_tcp_conns, max_unique_ips, data_quota_bytes, expiration,
       discovered_at, updated_at
FROM discovered_clients
WHERE id = $1;

-- name: GetDiscoveredClientByAgentAndName :one
SELECT id, agent_id, client_name, secret, status, total_octets,
       current_connections, active_unique_ips, connection_link,
       max_tcp_conns, max_unique_ips, data_quota_bytes, expiration,
       discovered_at, updated_at
FROM discovered_clients
WHERE agent_id = $1 AND client_name = $2;

-- name: ListDiscoveredClients :many
SELECT id, agent_id, client_name, secret, status, total_octets,
       current_connections, active_unique_ips, connection_link,
       max_tcp_conns, max_unique_ips, data_quota_bytes, expiration,
       discovered_at, updated_at
FROM discovered_clients
ORDER BY discovered_at DESC, id;

-- name: UpsertDiscoveredClient :exec
INSERT INTO discovered_clients (id, agent_id, client_name, secret, status,
                                total_octets, current_connections,
                                active_unique_ips, connection_link,
                                max_tcp_conns, max_unique_ips,
                                data_quota_bytes, expiration,
                                discovered_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
ON CONFLICT (id) DO UPDATE
SET secret              = EXCLUDED.secret,
    status              = EXCLUDED.status,
    total_octets        = EXCLUDED.total_octets,
    current_connections = EXCLUDED.current_connections,
    active_unique_ips   = EXCLUDED.active_unique_ips,
    connection_link     = EXCLUDED.connection_link,
    max_tcp_conns       = EXCLUDED.max_tcp_conns,
    max_unique_ips      = EXCLUDED.max_unique_ips,
    data_quota_bytes    = EXCLUDED.data_quota_bytes,
    expiration          = EXCLUDED.expiration,
    discovered_at       = EXCLUDED.discovered_at,
    updated_at          = EXCLUDED.updated_at;

-- name: UpdateDiscoveredClientStatus :execrows
UPDATE discovered_clients SET status = $2, updated_at = $3 WHERE id = $1;

-- name: DeleteDiscoveredClient :execrows
DELETE FROM discovered_clients WHERE id = $1;
