-- R-Q-03: client_usage — per-(client, agent) usage counters reported
-- back from agents.

-- name: ListClientUsageForClient :many
SELECT client_id, agent_id, traffic_used_bytes, unique_ips_used,
       active_tcp_conns, active_unique_ips, last_seq, observed_at
FROM client_usage
WHERE client_id = $1;

-- name: ListAllClientUsage :many
SELECT client_id, agent_id, traffic_used_bytes, unique_ips_used,
       active_tcp_conns, active_unique_ips, last_seq, observed_at
FROM client_usage
ORDER BY client_id ASC, agent_id ASC;


-- name: UpsertClientUsage :exec
INSERT INTO client_usage (client_id, agent_id, traffic_used_bytes,
                          unique_ips_used, active_tcp_conns,
                          active_unique_ips, last_seq, observed_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT (client_id, agent_id) DO UPDATE
SET traffic_used_bytes = EXCLUDED.traffic_used_bytes,
    unique_ips_used    = EXCLUDED.unique_ips_used,
    active_tcp_conns   = EXCLUDED.active_tcp_conns,
    active_unique_ips  = EXCLUDED.active_unique_ips,
    last_seq           = EXCLUDED.last_seq,
    observed_at        = EXCLUDED.observed_at;

-- name: DeleteClientUsageByClient :exec
DELETE FROM client_usage WHERE client_id = $1;
