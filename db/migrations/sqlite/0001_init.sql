CREATE TABLE IF NOT EXISTS users (
    id TEXT PRIMARY KEY,
    username TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    role TEXT NOT NULL,
    totp_enabled INTEGER NOT NULL DEFAULT 0,
    totp_secret TEXT NOT NULL DEFAULT '',
    created_at_unix INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS user_appearance (
    user_id TEXT PRIMARY KEY,
    theme TEXT NOT NULL DEFAULT 'system',
    density TEXT NOT NULL DEFAULT 'comfortable',
    updated_at_unix INTEGER NOT NULL DEFAULT 0,
    FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS fleet_groups (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    created_at_unix INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS agents (
    id TEXT PRIMARY KEY,
    node_name TEXT NOT NULL,
    fleet_group_id TEXT,
    version TEXT NOT NULL DEFAULT '',
    read_only INTEGER NOT NULL DEFAULT 0,
    last_seen_at_unix INTEGER NOT NULL,
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
    idempotency_key TEXT NOT NULL UNIQUE,
    payload_json TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS job_targets (
    job_id TEXT NOT NULL,
    agent_id TEXT NOT NULL,
    status TEXT NOT NULL,
    result_text TEXT NOT NULL DEFAULT '',
    result_json TEXT NOT NULL DEFAULT '',
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
    fleet_group_id TEXT,
    issued_at_unix INTEGER NOT NULL,
    expires_at_unix INTEGER NOT NULL,
    consumed_at_unix INTEGER,
    revoked_at_unix INTEGER
);

CREATE TABLE IF NOT EXISTS clients (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    secret_ciphertext TEXT NOT NULL,
    user_ad_tag TEXT NOT NULL,
    enabled INTEGER NOT NULL DEFAULT 1,
    max_tcp_conns INTEGER NOT NULL DEFAULT 0,
    max_unique_ips INTEGER NOT NULL DEFAULT 0,
    data_quota_bytes INTEGER NOT NULL DEFAULT 0,
    expiration_rfc3339 TEXT NOT NULL DEFAULT '',
    created_at_unix INTEGER NOT NULL,
    updated_at_unix INTEGER NOT NULL,
    deleted_at_unix INTEGER
);

CREATE TABLE IF NOT EXISTS client_assignments (
    id TEXT PRIMARY KEY,
    client_id TEXT NOT NULL,
    target_type TEXT NOT NULL,
    fleet_group_id TEXT,
    agent_id TEXT,
    created_at_unix INTEGER NOT NULL,
    FOREIGN KEY (client_id) REFERENCES clients (id),
    FOREIGN KEY (fleet_group_id) REFERENCES fleet_groups (id),
    FOREIGN KEY (agent_id) REFERENCES agents (id)
);

CREATE TABLE IF NOT EXISTS client_deployments (
    client_id TEXT NOT NULL,
    agent_id TEXT NOT NULL,
    desired_operation TEXT NOT NULL,
    status TEXT NOT NULL,
    last_error TEXT NOT NULL DEFAULT '',
    connection_link TEXT NOT NULL DEFAULT '',
    last_applied_at_unix INTEGER,
    updated_at_unix INTEGER NOT NULL,
    PRIMARY KEY (client_id, agent_id),
    FOREIGN KEY (client_id) REFERENCES clients (id),
    FOREIGN KEY (agent_id) REFERENCES agents (id)
);

CREATE TABLE IF NOT EXISTS panel_settings (
    scope TEXT PRIMARY KEY,
    http_public_url TEXT NOT NULL DEFAULT '',
    http_root_path TEXT NOT NULL DEFAULT '',
    grpc_public_endpoint TEXT NOT NULL DEFAULT '',
    http_listen_address TEXT NOT NULL DEFAULT '',
    grpc_listen_address TEXT NOT NULL DEFAULT '',
    tls_mode TEXT NOT NULL DEFAULT '',
    tls_cert_file TEXT NOT NULL DEFAULT '',
    tls_key_file TEXT NOT NULL DEFAULT '',
    updated_at_unix INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS certificate_authority (
    scope TEXT PRIMARY KEY,
    ca_pem TEXT NOT NULL,
    private_key_pem TEXT NOT NULL,
    updated_at_unix INTEGER NOT NULL
);
