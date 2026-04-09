package postgres

import "database/sql"

const initialSchema = `
CREATE TABLE IF NOT EXISTS fleet_groups (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS users (
    id TEXT PRIMARY KEY,
    username TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    role TEXT NOT NULL,
    totp_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    totp_secret TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS user_appearance (
    user_id TEXT PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    theme TEXT NOT NULL DEFAULT 'system',
    density TEXT NOT NULL DEFAULT 'comfortable',
    help_mode TEXT NOT NULL DEFAULT 'basic',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT TIMESTAMPTZ 'epoch'
);

CREATE TABLE IF NOT EXISTS agents (
    id TEXT PRIMARY KEY,
    node_name TEXT NOT NULL,
    fleet_group_id TEXT REFERENCES fleet_groups (id),
    version TEXT NOT NULL DEFAULT '',
    read_only BOOLEAN NOT NULL DEFAULT FALSE,
    last_seen_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS telemt_instances (
    id TEXT PRIMARY KEY,
    agent_id TEXT NOT NULL REFERENCES agents (id),
    name TEXT NOT NULL,
    version TEXT NOT NULL DEFAULT '',
    config_fingerprint TEXT NOT NULL DEFAULT '',
    connected_users BIGINT NOT NULL DEFAULT 0,
    read_only BOOLEAN NOT NULL DEFAULT FALSE,
    updated_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS telemt_runtime_current (
    agent_id TEXT PRIMARY KEY REFERENCES agents (id) ON DELETE CASCADE,
    observed_at TIMESTAMPTZ NOT NULL,
    state TEXT NOT NULL DEFAULT '',
    state_reason TEXT NOT NULL DEFAULT '',
    read_only BOOLEAN NOT NULL DEFAULT FALSE,
    accepting_new_connections BOOLEAN NOT NULL DEFAULT FALSE,
    me_runtime_ready BOOLEAN NOT NULL DEFAULT FALSE,
    me2dc_fallback_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    use_middle_proxy BOOLEAN NOT NULL DEFAULT FALSE,
    startup_status TEXT NOT NULL DEFAULT '',
    startup_stage TEXT NOT NULL DEFAULT '',
    startup_progress_pct DOUBLE PRECISION NOT NULL DEFAULT 0,
    initialization_status TEXT NOT NULL DEFAULT '',
    degraded BOOLEAN NOT NULL DEFAULT FALSE,
    initialization_stage TEXT NOT NULL DEFAULT '',
    initialization_progress_pct DOUBLE PRECISION NOT NULL DEFAULT 0,
    transport_mode TEXT NOT NULL DEFAULT '',
    current_connections BIGINT NOT NULL DEFAULT 0,
    current_connections_me BIGINT NOT NULL DEFAULT 0,
    current_connections_direct BIGINT NOT NULL DEFAULT 0,
    active_users BIGINT NOT NULL DEFAULT 0,
    uptime_seconds DOUBLE PRECISION NOT NULL DEFAULT 0,
    connections_total BIGINT NOT NULL DEFAULT 0,
    connections_bad_total BIGINT NOT NULL DEFAULT 0,
    handshake_timeouts_total BIGINT NOT NULL DEFAULT 0,
    configured_users BIGINT NOT NULL DEFAULT 0,
    dc_coverage_pct DOUBLE PRECISION NOT NULL DEFAULT 0,
    healthy_upstreams BIGINT NOT NULL DEFAULT 0,
    total_upstreams BIGINT NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS telemt_runtime_dcs_current (
    agent_id TEXT NOT NULL REFERENCES agents (id) ON DELETE CASCADE,
    dc BIGINT NOT NULL,
    observed_at TIMESTAMPTZ NOT NULL,
    available_endpoints BIGINT NOT NULL DEFAULT 0,
    available_pct DOUBLE PRECISION NOT NULL DEFAULT 0,
    required_writers BIGINT NOT NULL DEFAULT 0,
    alive_writers BIGINT NOT NULL DEFAULT 0,
    coverage_pct DOUBLE PRECISION NOT NULL DEFAULT 0,
    rtt_ms DOUBLE PRECISION NOT NULL DEFAULT 0,
    load DOUBLE PRECISION NOT NULL DEFAULT 0,
    PRIMARY KEY (agent_id, dc)
);

CREATE TABLE IF NOT EXISTS telemt_runtime_upstreams_current (
    agent_id TEXT NOT NULL REFERENCES agents (id) ON DELETE CASCADE,
    upstream_id BIGINT NOT NULL,
    observed_at TIMESTAMPTZ NOT NULL,
    route_kind TEXT NOT NULL DEFAULT '',
    address TEXT NOT NULL DEFAULT '',
    healthy BOOLEAN NOT NULL DEFAULT FALSE,
    fails BIGINT NOT NULL DEFAULT 0,
    effective_latency_ms DOUBLE PRECISION NOT NULL DEFAULT 0,
    PRIMARY KEY (agent_id, upstream_id)
);

CREATE TABLE IF NOT EXISTS telemt_runtime_events (
    agent_id TEXT NOT NULL REFERENCES agents (id) ON DELETE CASCADE,
    sequence BIGINT NOT NULL,
    observed_at TIMESTAMPTZ NOT NULL,
    timestamp_at TIMESTAMPTZ NOT NULL,
    event_type TEXT NOT NULL DEFAULT '',
    context TEXT NOT NULL DEFAULT '',
    severity TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (agent_id, sequence)
);

CREATE TABLE IF NOT EXISTS telemt_diagnostics_current (
    agent_id TEXT PRIMARY KEY REFERENCES agents (id) ON DELETE CASCADE,
    observed_at TIMESTAMPTZ NOT NULL,
    state TEXT NOT NULL DEFAULT '',
    state_reason TEXT NOT NULL DEFAULT '',
    system_info_json TEXT NOT NULL DEFAULT '{}',
    effective_limits_json TEXT NOT NULL DEFAULT '{}',
    security_posture_json TEXT NOT NULL DEFAULT '{}',
    minimal_all_json TEXT NOT NULL DEFAULT '{}',
    me_pool_json TEXT NOT NULL DEFAULT '{}',
    dcs_json TEXT NOT NULL DEFAULT '{}'
);

CREATE TABLE IF NOT EXISTS telemt_security_inventory_current (
    agent_id TEXT PRIMARY KEY REFERENCES agents (id) ON DELETE CASCADE,
    observed_at TIMESTAMPTZ NOT NULL,
    state TEXT NOT NULL DEFAULT '',
    state_reason TEXT NOT NULL DEFAULT '',
    enabled BOOLEAN NOT NULL DEFAULT FALSE,
    entries_total BIGINT NOT NULL DEFAULT 0,
    entries_json TEXT NOT NULL DEFAULT '[]'
);

CREATE TABLE IF NOT EXISTS telemt_detail_boosts (
    agent_id TEXT PRIMARY KEY REFERENCES agents (id) ON DELETE CASCADE,
    expires_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS jobs (
    id TEXT PRIMARY KEY,
    action TEXT NOT NULL,
    idempotency_key TEXT NOT NULL UNIQUE,
    actor_id TEXT NOT NULL,
    status TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    ttl_nanos BIGINT NOT NULL,
    payload_json TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS job_targets (
    job_id TEXT NOT NULL REFERENCES jobs (id),
    agent_id TEXT NOT NULL,
    status TEXT NOT NULL,
    result_text TEXT NOT NULL DEFAULT '',
    result_json TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (job_id, agent_id)
);

CREATE TABLE IF NOT EXISTS audit_events (
    id TEXT PRIMARY KEY,
    actor_id TEXT NOT NULL,
    action TEXT NOT NULL,
    target_id TEXT NOT NULL,
    details JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS metric_snapshots (
    id TEXT PRIMARY KEY,
    agent_id TEXT NOT NULL REFERENCES agents (id),
    instance_id TEXT NOT NULL DEFAULT '',
    captured_at TIMESTAMPTZ NOT NULL,
    values JSONB NOT NULL
);

CREATE TABLE IF NOT EXISTS enrollment_tokens (
    value TEXT PRIMARY KEY,
    fleet_group_id TEXT,
    issued_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    consumed_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS agent_certificate_recovery_grants (
    agent_id TEXT PRIMARY KEY REFERENCES agents (id),
    issued_by TEXT NOT NULL,
    issued_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    used_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS clients (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    secret_ciphertext TEXT NOT NULL,
    user_ad_tag TEXT NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    max_tcp_conns BIGINT NOT NULL DEFAULT 0,
    max_unique_ips BIGINT NOT NULL DEFAULT 0,
    data_quota_bytes BIGINT NOT NULL DEFAULT 0,
    expiration_rfc3339 TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    deleted_at TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS client_assignments (
    id TEXT PRIMARY KEY,
    client_id TEXT NOT NULL REFERENCES clients (id),
    target_type TEXT NOT NULL,
    fleet_group_id TEXT REFERENCES fleet_groups (id),
    agent_id TEXT REFERENCES agents (id),
    created_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS client_deployments (
    client_id TEXT NOT NULL REFERENCES clients (id),
    agent_id TEXT NOT NULL REFERENCES agents (id),
    desired_operation TEXT NOT NULL,
    status TEXT NOT NULL,
    last_error TEXT NOT NULL DEFAULT '',
    connection_link TEXT NOT NULL DEFAULT '',
    last_applied_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (client_id, agent_id)
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
    updated_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS certificate_authority (
    scope TEXT PRIMARY KEY,
    ca_pem TEXT NOT NULL,
    private_key_pem TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL
);
`

// Migrate applies the current PostgreSQL schema to the opened database.
func Migrate(db *sql.DB) error {
	if _, err := db.Exec(initialSchema); err != nil {
		return err
	}

	if _, err := db.Exec(`ALTER TABLE users ADD COLUMN IF NOT EXISTS totp_enabled BOOLEAN NOT NULL DEFAULT FALSE`); err != nil {
		return err
	}
	if _, err := db.Exec(`ALTER TABLE jobs ADD COLUMN IF NOT EXISTS payload_json TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	if _, err := db.Exec(`ALTER TABLE job_targets ADD COLUMN IF NOT EXISTS result_json TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	if _, err := db.Exec(`ALTER TABLE user_appearance ADD COLUMN IF NOT EXISTS help_mode TEXT NOT NULL DEFAULT 'basic'`); err != nil {
		return err
	}

	if _, err := db.Exec(`ALTER TABLE enrollment_tokens ADD COLUMN IF NOT EXISTS revoked_at TIMESTAMPTZ`); err != nil {
		return err
	}

	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS discovered_clients (
			id TEXT PRIMARY KEY,
			agent_id TEXT NOT NULL REFERENCES agents (id),
			client_name TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'pending_review',
			total_octets BIGINT NOT NULL DEFAULT 0,
			current_connections INTEGER NOT NULL DEFAULT 0,
			active_unique_ips INTEGER NOT NULL DEFAULT 0,
			connection_link TEXT NOT NULL DEFAULT '',
			max_tcp_conns INTEGER NOT NULL DEFAULT 0,
			max_unique_ips INTEGER NOT NULL DEFAULT 0,
			data_quota_bytes BIGINT NOT NULL DEFAULT 0,
			expiration TEXT NOT NULL DEFAULT '',
			discovered_at TIMESTAMPTZ NOT NULL,
			updated_at TIMESTAMPTZ NOT NULL,
			UNIQUE (agent_id, client_name)
		)
	`); err != nil {
		return err
	}

	// Align discovered_clients schema with SQLite (secret column).
	if _, err := db.Exec(`ALTER TABLE discovered_clients ADD COLUMN IF NOT EXISTS secret TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}

	// Add ON DELETE CASCADE to client_assignments and client_deployments FKs.
	// Best-effort: constraint names follow PostgreSQL auto-naming convention.
	db.Exec(`
		DO $$ BEGIN
			ALTER TABLE client_assignments DROP CONSTRAINT IF EXISTS client_assignments_client_id_fkey;
			ALTER TABLE client_assignments ADD CONSTRAINT client_assignments_client_id_fkey
				FOREIGN KEY (client_id) REFERENCES clients (id) ON DELETE CASCADE;
		EXCEPTION WHEN others THEN NULL;
		END $$
	`)
	db.Exec(`
		DO $$ BEGIN
			ALTER TABLE client_deployments DROP CONSTRAINT IF EXISTS client_deployments_client_id_fkey;
			ALTER TABLE client_deployments ADD CONSTRAINT client_deployments_client_id_fkey
				FOREIGN KEY (client_id) REFERENCES clients (id) ON DELETE CASCADE;
		EXCEPTION WHEN others THEN NULL;
		END $$
	`)

	// Performance indexes for frequently queried columns.
	_, err := db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_agents_last_seen_at ON agents (last_seen_at);
		CREATE INDEX IF NOT EXISTS idx_agents_fleet_group_id ON agents (fleet_group_id);
		CREATE INDEX IF NOT EXISTS idx_telemt_instances_agent_id ON telemt_instances (agent_id);
		CREATE INDEX IF NOT EXISTS idx_client_assignments_client_id ON client_assignments (client_id);
		CREATE INDEX IF NOT EXISTS idx_client_deployments_client_id ON client_deployments (client_id);
		CREATE INDEX IF NOT EXISTS idx_jobs_created_at ON jobs (created_at);
		CREATE INDEX IF NOT EXISTS idx_audit_events_created_at ON audit_events (created_at);
		CREATE INDEX IF NOT EXISTS idx_metric_snapshots_agent_captured ON metric_snapshots (agent_id, captured_at);
		CREATE INDEX IF NOT EXISTS idx_discovered_clients_agent_id ON discovered_clients (agent_id)
	`)
	if err != nil {
		return err
	}

	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS ts_server_load (
			agent_id                TEXT NOT NULL,
			captured_at             TIMESTAMPTZ NOT NULL,
			cpu_pct_avg             REAL NOT NULL DEFAULT 0,
			cpu_pct_max             REAL NOT NULL DEFAULT 0,
			mem_pct_avg             REAL NOT NULL DEFAULT 0,
			mem_pct_max             REAL NOT NULL DEFAULT 0,
			disk_pct_avg            REAL NOT NULL DEFAULT 0,
			disk_pct_max            REAL NOT NULL DEFAULT 0,
			load_1m                 REAL NOT NULL DEFAULT 0,
			load_5m                 REAL NOT NULL DEFAULT 0,
			load_15m                REAL NOT NULL DEFAULT 0,
			connections_avg         INTEGER NOT NULL DEFAULT 0,
			connections_max         INTEGER NOT NULL DEFAULT 0,
			connections_me_avg      INTEGER NOT NULL DEFAULT 0,
			connections_direct_avg  INTEGER NOT NULL DEFAULT 0,
			active_users_avg        INTEGER NOT NULL DEFAULT 0,
			active_users_max        INTEGER NOT NULL DEFAULT 0,
			connections_total       BIGINT NOT NULL DEFAULT 0,
			connections_bad_total   BIGINT NOT NULL DEFAULT 0,
			handshake_timeouts_total BIGINT NOT NULL DEFAULT 0,
			dc_coverage_min_pct     REAL NOT NULL DEFAULT 0,
			dc_coverage_avg_pct     REAL NOT NULL DEFAULT 0,
			healthy_upstreams       INTEGER NOT NULL DEFAULT 0,
			total_upstreams         INTEGER NOT NULL DEFAULT 0,
			sample_count            INTEGER NOT NULL DEFAULT 1,
			PRIMARY KEY (agent_id, captured_at)
		);
		CREATE INDEX IF NOT EXISTS idx_ts_server_load_time ON ts_server_load (agent_id, captured_at DESC);

		CREATE TABLE IF NOT EXISTS ts_dc_health (
			agent_id         TEXT NOT NULL,
			captured_at      TIMESTAMPTZ NOT NULL,
			dc               INTEGER NOT NULL,
			coverage_pct_avg REAL NOT NULL DEFAULT 0,
			coverage_pct_min REAL NOT NULL DEFAULT 0,
			rtt_ms_avg       REAL NOT NULL DEFAULT 0,
			rtt_ms_max       REAL NOT NULL DEFAULT 0,
			alive_writers_min INTEGER NOT NULL DEFAULT 0,
			required_writers INTEGER NOT NULL DEFAULT 0,
			load_max         INTEGER NOT NULL DEFAULT 0,
			sample_count     INTEGER NOT NULL DEFAULT 1,
			PRIMARY KEY (agent_id, dc, captured_at)
		);
		CREATE INDEX IF NOT EXISTS idx_ts_dc_health_time ON ts_dc_health (agent_id, captured_at DESC);

		CREATE TABLE IF NOT EXISTS client_ip_history (
			agent_id    TEXT NOT NULL,
			client_id   TEXT NOT NULL,
			ip_address  TEXT NOT NULL,
			first_seen  TIMESTAMPTZ NOT NULL,
			last_seen   TIMESTAMPTZ NOT NULL,
			PRIMARY KEY (agent_id, client_id, ip_address)
		);
		CREATE INDEX IF NOT EXISTS idx_client_ip_last_seen ON client_ip_history (last_seen);
		CREATE INDEX IF NOT EXISTS idx_client_ip_client ON client_ip_history (client_id, last_seen DESC)
	`)
	return err
}
