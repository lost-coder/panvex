-- R-Q-03: client_usage — per-(client, agent) usage counters reported
-- back from agents.

-- name: ListClientUsageForClient :many
-- Column order matches the physical table (quota_* were ALTER-appended by
-- 0043, watermark columns by 0057) so sqlc maps the row straight onto
-- dbsqlc.ClientUsage.
SELECT client_id, agent_id, traffic_used_bytes, unique_ips_used,
       active_tcp_conns, active_unique_ips, last_seq, observed_at,
       quota_used_bytes, quota_last_reset_unix, agent_boot_id,
       last_total_bytes
FROM client_usage
WHERE client_id = $1;

-- name: ListAllClientUsage :many
SELECT client_id, agent_id, traffic_used_bytes, unique_ips_used,
       active_tcp_conns, active_unique_ips, last_seq, observed_at,
       quota_used_bytes, quota_last_reset_unix, agent_boot_id,
       last_total_bytes
FROM client_usage
ORDER BY client_id ASC, agent_id ASC;


-- name: UpsertClientUsage :exec
-- last_seq is the agent's per-connection report cursor; the DO UPDATE only
-- fires when the incoming last_seq is strictly newer than the stored one
-- (monotonicity guard — dies with the seq protocol in P4 задача 4).
-- agent_boot_id / last_total_bytes carry the P4 cumulative-counter
-- watermark alongside.
INSERT INTO client_usage (client_id, agent_id, traffic_used_bytes,
                          unique_ips_used, active_tcp_conns,
                          active_unique_ips, quota_used_bytes,
                          quota_last_reset_unix, last_seq, observed_at,
                          agent_boot_id, last_total_bytes)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
ON CONFLICT (client_id, agent_id) DO UPDATE
SET traffic_used_bytes    = EXCLUDED.traffic_used_bytes,
    unique_ips_used       = EXCLUDED.unique_ips_used,
    active_tcp_conns      = EXCLUDED.active_tcp_conns,
    active_unique_ips     = EXCLUDED.active_unique_ips,
    quota_used_bytes      = EXCLUDED.quota_used_bytes,
    quota_last_reset_unix = EXCLUDED.quota_last_reset_unix,
    last_seq              = EXCLUDED.last_seq,
    observed_at           = EXCLUDED.observed_at,
    agent_boot_id         = EXCLUDED.agent_boot_id,
    last_total_bytes      = EXCLUDED.last_total_bytes
WHERE EXCLUDED.last_seq > client_usage.last_seq;

-- name: DeleteClientUsageByClient :exec
DELETE FROM client_usage WHERE client_id = $1;
