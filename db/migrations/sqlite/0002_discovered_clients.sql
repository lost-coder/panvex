-- +goose Up
CREATE TABLE IF NOT EXISTS discovered_clients (
    id TEXT PRIMARY KEY,
    agent_id TEXT NOT NULL,
    client_name TEXT NOT NULL,
    secret TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'pending_review',
    total_octets INTEGER NOT NULL DEFAULT 0,
    current_connections INTEGER NOT NULL DEFAULT 0,
    active_unique_ips INTEGER NOT NULL DEFAULT 0,
    connection_link TEXT NOT NULL DEFAULT '',
    max_tcp_conns INTEGER NOT NULL DEFAULT 0,
    max_unique_ips INTEGER NOT NULL DEFAULT 0,
    data_quota_bytes INTEGER NOT NULL DEFAULT 0,
    expiration TEXT NOT NULL DEFAULT '',
    discovered_at_unix INTEGER NOT NULL,
    updated_at_unix INTEGER NOT NULL,
    UNIQUE (agent_id, client_name),
    FOREIGN KEY (agent_id) REFERENCES agents (id)
);

-- +goose Down
DROP TABLE IF EXISTS discovered_clients;
