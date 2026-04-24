-- +goose Up
-- Persist per-(client, agent) usage counters so they survive a panel
-- restart. Previously these lived only in server.clientUsage (an
-- in-memory map) and got re-seeded from discovered_clients.total_octets
-- as a stop-gap — fragile because that fallback depends on the agent
-- still being online and re-discovering the client.
--
-- The row is keyed by (client_id, agent_id) to match the in-memory
-- shape; aggregate totals across nodes are computed at read time.
-- last_seq carries the per-agent delta cursor (rewinds to 1 on agent
-- restart; the higher value wins) so dedupe works across CP restarts.
CREATE TABLE IF NOT EXISTS client_usage (
    client_id         TEXT NOT NULL REFERENCES clients (id) ON DELETE CASCADE,
    agent_id          TEXT NOT NULL REFERENCES agents (id) ON DELETE CASCADE,
    traffic_used_bytes BIGINT NOT NULL DEFAULT 0,
    unique_ips_used   INTEGER NOT NULL DEFAULT 0,
    active_tcp_conns  INTEGER NOT NULL DEFAULT 0,
    active_unique_ips INTEGER NOT NULL DEFAULT 0,
    last_seq          BIGINT NOT NULL DEFAULT 0,
    observed_at       TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (client_id, agent_id)
);

CREATE INDEX IF NOT EXISTS idx_client_usage_agent_id
    ON client_usage (agent_id);

-- +goose Down
DROP INDEX IF EXISTS idx_client_usage_agent_id;
DROP TABLE IF EXISTS client_usage;
