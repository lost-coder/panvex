-- R-Q-03: client_ip_history — durable per-(client, agent, ip) seen
-- log used by the IP history card.

-- name: ListClientIPHistory :many
SELECT agent_id, client_id, ip_address, first_seen, last_seen
FROM client_ip_history
WHERE client_id = $1
ORDER BY last_seen DESC;

-- name: UpsertClientIPHistory :exec
INSERT INTO client_ip_history (agent_id, client_id, ip_address, first_seen, last_seen)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (agent_id, client_id, ip_address) DO UPDATE
SET last_seen = EXCLUDED.last_seen;

-- name: PruneClientIPHistory :execrows
DELETE FROM client_ip_history WHERE last_seen < $1;
