CREATE TABLE IF NOT EXISTS users (
    id TEXT PRIMARY KEY,
    username TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    role TEXT NOT NULL,
    totp_enabled INTEGER NOT NULL DEFAULT 0,
    totp_secret TEXT NOT NULL DEFAULT '',
    created_at_unix INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS environments (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    created_at_unix INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS fleet_groups (
    id TEXT PRIMARY KEY,
    environment_id TEXT NOT NULL,
    name TEXT NOT NULL,
    created_at_unix INTEGER NOT NULL,
    FOREIGN KEY (environment_id) REFERENCES environments (id)
);

CREATE TABLE IF NOT EXISTS agents (
    id TEXT PRIMARY KEY,
    node_name TEXT NOT NULL,
    environment_id TEXT NOT NULL,
    fleet_group_id TEXT NOT NULL,
    version TEXT NOT NULL DEFAULT '',
    read_only INTEGER NOT NULL DEFAULT 0,
    last_seen_at_unix INTEGER NOT NULL,
    FOREIGN KEY (environment_id) REFERENCES environments (id),
    FOREIGN KEY (fleet_group_id) REFERENCES fleet_groups (id)
);

CREATE TABLE IF NOT EXISTS telemt_instances (
    id TEXT PRIMARY KEY,
    agent_id TEXT NOT NULL,
    name TEXT NOT NULL,
    version TEXT NOT NULL DEFAULT '',
    config_fingerprint TEXT NOT NULL DEFAULT '',
    connected_users INTEGER NOT NULL DEFAULT 0,
    read_only INTEGER NOT NULL DEFAULT 0,
    updated_at_unix INTEGER NOT NULL,
    FOREIGN KEY (agent_id) REFERENCES agents (id)
);

CREATE TABLE IF NOT EXISTS jobs (
    id TEXT PRIMARY KEY,
    action TEXT NOT NULL,
    actor_id TEXT NOT NULL,
    status TEXT NOT NULL,
    created_at_unix INTEGER NOT NULL,
    ttl_nanos INTEGER NOT NULL,
    idempotency_key TEXT NOT NULL UNIQUE
);

CREATE TABLE IF NOT EXISTS job_targets (
    job_id TEXT NOT NULL,
    agent_id TEXT NOT NULL,
    status TEXT NOT NULL,
    result_text TEXT NOT NULL DEFAULT '',
    updated_at_unix INTEGER NOT NULL,
    PRIMARY KEY (job_id, agent_id),
    FOREIGN KEY (job_id) REFERENCES jobs (id)
);

CREATE TABLE IF NOT EXISTS audit_events (
    id TEXT PRIMARY KEY,
    actor_id TEXT NOT NULL,
    action TEXT NOT NULL,
    target_id TEXT NOT NULL,
    created_at_unix INTEGER NOT NULL,
    details_json TEXT NOT NULL DEFAULT '{}'
);

CREATE TABLE IF NOT EXISTS metric_snapshots (
    id TEXT PRIMARY KEY,
    agent_id TEXT NOT NULL,
    instance_id TEXT NOT NULL DEFAULT '',
    captured_at_unix INTEGER NOT NULL,
    values_json TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS enrollment_tokens (
    value TEXT PRIMARY KEY,
    environment_id TEXT NOT NULL,
    fleet_group_id TEXT NOT NULL,
    issued_at_unix INTEGER NOT NULL,
    expires_at_unix INTEGER NOT NULL,
    consumed_at_unix INTEGER,
    revoked_at_unix INTEGER
);
