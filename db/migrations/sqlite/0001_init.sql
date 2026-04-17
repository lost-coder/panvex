-- +goose Up
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
    help_mode TEXT NOT NULL DEFAULT 'basic',
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
    created_at_unix INTEGER NOT NULL DEFAULT 0,
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

CREATE TABLE IF NOT EXISTS telemt_runtime_current (
    agent_id TEXT PRIMARY KEY,
    observed_at_unix INTEGER NOT NULL,
    state TEXT NOT NULL DEFAULT '',
    state_reason TEXT NOT NULL DEFAULT '',
    read_only INTEGER NOT NULL DEFAULT 0,
    accepting_new_connections INTEGER NOT NULL DEFAULT 0,
    me_runtime_ready INTEGER NOT NULL DEFAULT 0,
    me2dc_fallback_enabled INTEGER NOT NULL DEFAULT 0,
    use_middle_proxy INTEGER NOT NULL DEFAULT 0,
    startup_status TEXT NOT NULL DEFAULT '',
    startup_stage TEXT NOT NULL DEFAULT '',
    startup_progress_pct REAL NOT NULL DEFAULT 0,
    initialization_status TEXT NOT NULL DEFAULT '',
    degraded INTEGER NOT NULL DEFAULT 0,
    initialization_stage TEXT NOT NULL DEFAULT '',
    initialization_progress_pct REAL NOT NULL DEFAULT 0,
    transport_mode TEXT NOT NULL DEFAULT '',
    current_connections INTEGER NOT NULL DEFAULT 0,
    current_connections_me INTEGER NOT NULL DEFAULT 0,
    current_connections_direct INTEGER NOT NULL DEFAULT 0,
    active_users INTEGER NOT NULL DEFAULT 0,
    uptime_seconds REAL NOT NULL DEFAULT 0,
    connections_total INTEGER NOT NULL DEFAULT 0,
    connections_bad_total INTEGER NOT NULL DEFAULT 0,
    handshake_timeouts_total INTEGER NOT NULL DEFAULT 0,
    configured_users INTEGER NOT NULL DEFAULT 0,
    dc_coverage_pct REAL NOT NULL DEFAULT 0,
    healthy_upstreams INTEGER NOT NULL DEFAULT 0,
    total_upstreams INTEGER NOT NULL DEFAULT 0,
    FOREIGN KEY (agent_id) REFERENCES agents (id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS telemt_runtime_dcs_current (
    agent_id TEXT NOT NULL,
    dc INTEGER NOT NULL,
    observed_at_unix INTEGER NOT NULL,
    available_endpoints INTEGER NOT NULL DEFAULT 0,
    available_pct REAL NOT NULL DEFAULT 0,
    required_writers INTEGER NOT NULL DEFAULT 0,
    alive_writers INTEGER NOT NULL DEFAULT 0,
    coverage_pct REAL NOT NULL DEFAULT 0,
    rtt_ms REAL NOT NULL DEFAULT 0,
    load REAL NOT NULL DEFAULT 0,
    PRIMARY KEY (agent_id, dc),
    FOREIGN KEY (agent_id) REFERENCES agents (id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS telemt_runtime_upstreams_current (
    agent_id TEXT NOT NULL,
    upstream_id INTEGER NOT NULL,
    observed_at_unix INTEGER NOT NULL,
    route_kind TEXT NOT NULL DEFAULT '',
    address TEXT NOT NULL DEFAULT '',
    healthy INTEGER NOT NULL DEFAULT 0,
    fails INTEGER NOT NULL DEFAULT 0,
    effective_latency_ms REAL NOT NULL DEFAULT 0,
    PRIMARY KEY (agent_id, upstream_id),
    FOREIGN KEY (agent_id) REFERENCES agents (id) ON DELETE CASCADE
);

-- Column name differs between drivers: SQLite uses timestamp_unix (INTEGER)
-- while Postgres uses timestamp_at (TIMESTAMPTZ). Query implementations in
-- each driver's telemetry.go must use the correct column name.
CREATE TABLE IF NOT EXISTS telemt_runtime_events (
    agent_id TEXT NOT NULL,
    sequence INTEGER NOT NULL,
    observed_at_unix INTEGER NOT NULL,
    timestamp_unix INTEGER NOT NULL,
    event_type TEXT NOT NULL DEFAULT '',
    context TEXT NOT NULL DEFAULT '',
    severity TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (agent_id, sequence),
    FOREIGN KEY (agent_id) REFERENCES agents (id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS telemt_diagnostics_current (
    agent_id TEXT PRIMARY KEY,
    observed_at_unix INTEGER NOT NULL,
    state TEXT NOT NULL DEFAULT '',
    state_reason TEXT NOT NULL DEFAULT '',
    system_info_json TEXT NOT NULL DEFAULT '{}',
    effective_limits_json TEXT NOT NULL DEFAULT '{}',
    security_posture_json TEXT NOT NULL DEFAULT '{}',
    minimal_all_json TEXT NOT NULL DEFAULT '{}',
    me_pool_json TEXT NOT NULL DEFAULT '{}',
    dcs_json TEXT NOT NULL DEFAULT '{}',
    FOREIGN KEY (agent_id) REFERENCES agents (id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS telemt_security_inventory_current (
    agent_id TEXT PRIMARY KEY,
    observed_at_unix INTEGER NOT NULL,
    state TEXT NOT NULL DEFAULT '',
    state_reason TEXT NOT NULL DEFAULT '',
    enabled INTEGER NOT NULL DEFAULT 0,
    entries_total INTEGER NOT NULL DEFAULT 0,
    entries_json TEXT NOT NULL DEFAULT '[]',
    FOREIGN KEY (agent_id) REFERENCES agents (id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS telemt_detail_boosts (
    agent_id TEXT PRIMARY KEY,
    expires_at_unix INTEGER NOT NULL,
    updated_at_unix INTEGER NOT NULL,
    FOREIGN KEY (agent_id) REFERENCES agents (id) ON DELETE CASCADE
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

CREATE TABLE IF NOT EXISTS agent_certificate_recovery_grants (
    agent_id TEXT PRIMARY KEY,
    issued_by TEXT NOT NULL,
    issued_at_unix INTEGER NOT NULL,
    expires_at_unix INTEGER NOT NULL,
    used_at_unix INTEGER,
    revoked_at_unix INTEGER,
    FOREIGN KEY (agent_id) REFERENCES agents (id)
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

-- +goose Down
DROP TABLE IF EXISTS certificate_authority;
DROP TABLE IF EXISTS panel_settings;
DROP TABLE IF EXISTS client_deployments;
DROP TABLE IF EXISTS client_assignments;
DROP TABLE IF EXISTS clients;
DROP TABLE IF EXISTS agent_certificate_recovery_grants;
DROP TABLE IF EXISTS enrollment_tokens;
DROP TABLE IF EXISTS metric_snapshots;
DROP TABLE IF EXISTS audit_events;
DROP TABLE IF EXISTS job_targets;
DROP TABLE IF EXISTS jobs;
DROP TABLE IF EXISTS telemt_detail_boosts;
DROP TABLE IF EXISTS telemt_security_inventory_current;
DROP TABLE IF EXISTS telemt_diagnostics_current;
DROP TABLE IF EXISTS telemt_runtime_events;
DROP TABLE IF EXISTS telemt_runtime_upstreams_current;
DROP TABLE IF EXISTS telemt_runtime_dcs_current;
DROP TABLE IF EXISTS telemt_runtime_current;
DROP TABLE IF EXISTS telemt_instances;
DROP TABLE IF EXISTS agents;
DROP TABLE IF EXISTS user_appearance;
DROP TABLE IF EXISTS users;
DROP TABLE IF EXISTS fleet_groups;
