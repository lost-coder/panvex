CREATE TABLE environments (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE fleet_groups (
    id TEXT PRIMARY KEY,
    environment_id TEXT NOT NULL REFERENCES environments (id),
    name TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE local_users (
    id TEXT PRIMARY KEY,
    username TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    role TEXT NOT NULL,
    totp_secret TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE agents (
    id TEXT PRIMARY KEY,
    node_name TEXT NOT NULL,
    environment_id TEXT NOT NULL REFERENCES environments (id),
    fleet_group_id TEXT NOT NULL REFERENCES fleet_groups (id),
    version TEXT NOT NULL DEFAULT '',
    read_only BOOLEAN NOT NULL DEFAULT FALSE,
    last_seen_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE telemt_instances (
    id TEXT PRIMARY KEY,
    agent_id TEXT NOT NULL REFERENCES agents (id),
    name TEXT NOT NULL,
    version TEXT NOT NULL DEFAULT '',
    config_fingerprint TEXT NOT NULL DEFAULT '',
    connected_users BIGINT NOT NULL DEFAULT 0,
    read_only BOOLEAN NOT NULL DEFAULT FALSE,
    updated_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE jobs (
    id TEXT PRIMARY KEY,
    action TEXT NOT NULL,
    target_agent_ids TEXT[] NOT NULL,
    idempotency_key TEXT NOT NULL UNIQUE,
    actor_id TEXT NOT NULL,
    status TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    ttl_seconds BIGINT NOT NULL
);

CREATE TABLE audit_events (
    id TEXT PRIMARY KEY,
    actor_id TEXT NOT NULL,
    action TEXT NOT NULL,
    target_id TEXT NOT NULL,
    details JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE metric_snapshots (
    id TEXT PRIMARY KEY,
    agent_id TEXT NOT NULL REFERENCES agents (id),
    instance_id TEXT NOT NULL DEFAULT '',
    captured_at TIMESTAMPTZ NOT NULL,
    values JSONB NOT NULL
);
