-- +goose Up
-- SQLite mirror of 0015 (Postgres). Schema-sync parity is enforced by
-- TestSchemaSyncPostgresMatchesSQLite. Timestamps use TEXT (ISO-8601)
-- like the rest of the SQLite bundle; BIGINT collapses to INTEGER.
CREATE TABLE IF NOT EXISTS client_usage (
    client_id         TEXT NOT NULL,
    agent_id          TEXT NOT NULL,
    traffic_used_bytes INTEGER NOT NULL DEFAULT 0,
    unique_ips_used   INTEGER NOT NULL DEFAULT 0,
    active_tcp_conns  INTEGER NOT NULL DEFAULT 0,
    active_unique_ips INTEGER NOT NULL DEFAULT 0,
    last_seq          INTEGER NOT NULL DEFAULT 0,
    observed_at_unix  INTEGER NOT NULL,
    PRIMARY KEY (client_id, agent_id),
    FOREIGN KEY (client_id) REFERENCES clients (id) ON DELETE CASCADE,
    FOREIGN KEY (agent_id) REFERENCES agents (id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_client_usage_agent_id
    ON client_usage (agent_id);

-- +goose Down
DROP INDEX IF EXISTS idx_client_usage_agent_id;
DROP TABLE IF EXISTS client_usage;
