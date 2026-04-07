package sqlite

import "database/sql"

const initialSchema = `
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
`

// Migrate applies the current SQLite schema to the opened database.
func Migrate(db *sql.DB) error {
	if _, err := db.Exec(initialSchema); err != nil {
		return err
	}

	if err := ensureUsersTotpEnabledColumn(db); err != nil {
		return err
	}
	if err := ensureEnrollmentTokensRevokedAtColumn(db); err != nil {
		return err
	}
	if err := ensureJobsPayloadJSONColumn(db); err != nil {
		return err
	}
	if err := ensureUserAppearanceHelpModeColumn(db); err != nil {
		return err
	}

	if err := ensureJobTargetsResultJSONColumn(db); err != nil {
		return err
	}

	_, err := db.Exec(`
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
		)
	`)
	if err != nil {
		return err
	}

	// Add secret column to existing discovered_clients tables (migration).
	if err := ensureDiscoveredClientsSecretColumn(db); err != nil {
		return err
	}

	// Align agents schema with PostgreSQL (created_at_unix column).
	if err := ensureAgentsCreatedAtColumn(db); err != nil {
		return err
	}

	return ensureIndexes(db)
}

func ensureDiscoveredClientsSecretColumn(db *sql.DB) error {
	rows, err := db.Query(`PRAGMA table_info(discovered_clients)`)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var cid int
		var name, columnType string
		var notNull int
		var defaultValue any
		var primaryKey int
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			return err
		}
		if name == "secret" {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}

	_, err = db.Exec(`ALTER TABLE discovered_clients ADD COLUMN secret TEXT NOT NULL DEFAULT ''`)
	return err
}

func ensureUsersTotpEnabledColumn(db *sql.DB) error {
	rows, err := db.Query(`PRAGMA table_info(users)`)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var cid int
		var name string
		var columnType string
		var notNull int
		var defaultValue any
		var primaryKey int
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			return err
		}
		if name == "totp_enabled" {
			return nil
		}
	}

	if err := rows.Err(); err != nil {
		return err
	}

	_, err = db.Exec(`ALTER TABLE users ADD COLUMN totp_enabled INTEGER NOT NULL DEFAULT 0`)
	return err
}

func ensureEnrollmentTokensRevokedAtColumn(db *sql.DB) error {
	rows, err := db.Query(`PRAGMA table_info(enrollment_tokens)`)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var cid int
		var name string
		var columnType string
		var notNull int
		var defaultValue any
		var primaryKey int
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			return err
		}
		if name == "revoked_at_unix" {
			return nil
		}
	}

	if err := rows.Err(); err != nil {
		return err
	}

	_, err = db.Exec(`ALTER TABLE enrollment_tokens ADD COLUMN revoked_at_unix INTEGER`)
	return err
}

func ensureJobsPayloadJSONColumn(db *sql.DB) error {
	rows, err := db.Query(`PRAGMA table_info(jobs)`)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var cid int
		var name string
		var columnType string
		var notNull int
		var defaultValue any
		var primaryKey int
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			return err
		}
		if name == "payload_json" {
			return nil
		}
	}

	if err := rows.Err(); err != nil {
		return err
	}

	_, err = db.Exec(`ALTER TABLE jobs ADD COLUMN payload_json TEXT NOT NULL DEFAULT ''`)
	return err
}

func ensureJobTargetsResultJSONColumn(db *sql.DB) error {
	rows, err := db.Query(`PRAGMA table_info(job_targets)`)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var cid int
		var name string
		var columnType string
		var notNull int
		var defaultValue any
		var primaryKey int
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			return err
		}
		if name == "result_json" {
			return nil
		}
	}

	if err := rows.Err(); err != nil {
		return err
	}

	_, err = db.Exec(`ALTER TABLE job_targets ADD COLUMN result_json TEXT NOT NULL DEFAULT ''`)
	return err
}

func ensureUserAppearanceHelpModeColumn(db *sql.DB) error {
	rows, err := db.Query(`PRAGMA table_info(user_appearance)`)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var cid int
		var name string
		var columnType string
		var notNull int
		var defaultValue any
		var primaryKey int
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			return err
		}
		if name == "help_mode" {
			return nil
		}
	}

	if err := rows.Err(); err != nil {
		return err
	}

	_, err = db.Exec(`ALTER TABLE user_appearance ADD COLUMN help_mode TEXT NOT NULL DEFAULT 'basic'`)
	return err
}

func ensureAgentsCreatedAtColumn(db *sql.DB) error {
	rows, err := db.Query(`PRAGMA table_info(agents)`)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var cid int
		var name string
		var columnType string
		var notNull int
		var defaultValue any
		var primaryKey int
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			return err
		}
		if name == "created_at_unix" {
			return nil
		}
	}

	if err := rows.Err(); err != nil {
		return err
	}

	_, err = db.Exec(`ALTER TABLE agents ADD COLUMN created_at_unix INTEGER NOT NULL DEFAULT 0`)
	return err
}

// ensureIndexes creates performance indexes for frequently queried columns.
func ensureIndexes(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE INDEX IF NOT EXISTS idx_agents_last_seen_at ON agents (last_seen_at_unix);
		CREATE INDEX IF NOT EXISTS idx_agents_fleet_group_id ON agents (fleet_group_id);
		CREATE INDEX IF NOT EXISTS idx_telemt_instances_agent_id ON telemt_instances (agent_id);
		CREATE INDEX IF NOT EXISTS idx_client_assignments_client_id ON client_assignments (client_id);
		CREATE INDEX IF NOT EXISTS idx_client_deployments_client_id ON client_deployments (client_id);
		CREATE INDEX IF NOT EXISTS idx_jobs_created_at ON jobs (created_at_unix);
		CREATE INDEX IF NOT EXISTS idx_audit_events_created_at ON audit_events (created_at_unix);
		CREATE INDEX IF NOT EXISTS idx_metric_snapshots_agent_captured ON metric_snapshots (agent_id, captured_at_unix);
		CREATE INDEX IF NOT EXISTS idx_discovered_clients_agent_id ON discovered_clients (agent_id)
	`)
	return err
}
