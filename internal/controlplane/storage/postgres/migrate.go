package postgres

import "database/sql"

const initialSchema = `
CREATE TABLE IF NOT EXISTS environments (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS fleet_groups (
    id TEXT PRIMARY KEY,
    environment_id TEXT NOT NULL REFERENCES environments (id),
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

CREATE TABLE IF NOT EXISTS agents (
    id TEXT PRIMARY KEY,
    node_name TEXT NOT NULL,
    environment_id TEXT NOT NULL REFERENCES environments (id),
    fleet_group_id TEXT NOT NULL REFERENCES fleet_groups (id),
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

CREATE TABLE IF NOT EXISTS jobs (
    id TEXT PRIMARY KEY,
    action TEXT NOT NULL,
    idempotency_key TEXT NOT NULL UNIQUE,
    actor_id TEXT NOT NULL,
    status TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    ttl_nanos BIGINT NOT NULL
);

CREATE TABLE IF NOT EXISTS job_targets (
    job_id TEXT NOT NULL REFERENCES jobs (id),
    agent_id TEXT NOT NULL,
    status TEXT NOT NULL,
    result_text TEXT NOT NULL DEFAULT '',
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
    environment_id TEXT NOT NULL,
    fleet_group_id TEXT NOT NULL,
    issued_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    consumed_at TIMESTAMPTZ,
    revoked_at TIMESTAMPTZ
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

	_, err := db.Exec(`ALTER TABLE enrollment_tokens ADD COLUMN IF NOT EXISTS revoked_at TIMESTAMPTZ`)
	return err
}
