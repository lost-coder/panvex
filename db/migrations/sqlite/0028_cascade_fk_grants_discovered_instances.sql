-- +goose Up
-- +goose NO TRANSACTION
-- Add ON DELETE CASCADE to the remaining FKs that point at agents (id):
--   * telemt_instances           — 0022 was a no-op based on the wrong
--                                  assumption that 0012_cascade_fk had
--                                  rebuilt this table; it had not.
--   * agent_certificate_recovery_grants — never patched.
--   * discovered_clients         — rebuilt in 0026 without cascade.
--
-- Without these, DELETE FROM agents fails with a FOREIGN KEY constraint
-- error whenever any of these tables hold rows for the deregistered agent
-- (see http_agents.persistAgentDeregister).
--
-- Dev-stage policy (matches 0012/0026): orphan rows are purged before
-- the rebuild so the new constraint does not abort the copy.

PRAGMA foreign_keys = OFF;

-- ─── telemt_instances ────────────────────────────────────────────────
-- Each table rebuild below is wrapped in its own explicit transaction so a
-- crash between DROP and RENAME can never leave a table dropped-but-not-
-- renamed. PRAGMA foreign_keys stays outside every BEGIN/COMMIT — SQLite
-- forbids toggling it inside a transaction.
DELETE FROM telemt_instances WHERE agent_id NOT IN (SELECT id FROM agents);

BEGIN;

CREATE TABLE telemt_instances_new (
    id TEXT PRIMARY KEY,
    agent_id TEXT NOT NULL,
    name TEXT NOT NULL,
    version TEXT NOT NULL DEFAULT '',
    config_fingerprint TEXT NOT NULL DEFAULT '',
    connected_users INTEGER NOT NULL DEFAULT 0,
    read_only INTEGER NOT NULL DEFAULT 0,
    updated_at_unix INTEGER NOT NULL,
    FOREIGN KEY (agent_id) REFERENCES agents (id) ON DELETE CASCADE
);

INSERT INTO telemt_instances_new (id, agent_id, name, version, config_fingerprint, connected_users, read_only, updated_at_unix)
SELECT id, agent_id, name, version, config_fingerprint, connected_users, read_only, updated_at_unix FROM telemt_instances;

DROP TABLE telemt_instances;
ALTER TABLE telemt_instances_new RENAME TO telemt_instances;

CREATE INDEX IF NOT EXISTS idx_telemt_instances_agent_id ON telemt_instances (agent_id);

COMMIT;

-- ─── agent_certificate_recovery_grants ───────────────────────────────
DELETE FROM agent_certificate_recovery_grants WHERE agent_id NOT IN (SELECT id FROM agents);

BEGIN;

CREATE TABLE agent_certificate_recovery_grants_new (
    agent_id TEXT PRIMARY KEY,
    issued_by TEXT NOT NULL,
    issued_at_unix INTEGER NOT NULL,
    expires_at_unix INTEGER NOT NULL,
    used_at_unix INTEGER,
    revoked_at_unix INTEGER,
    FOREIGN KEY (agent_id) REFERENCES agents (id) ON DELETE CASCADE
);

INSERT INTO agent_certificate_recovery_grants_new (agent_id, issued_by, issued_at_unix, expires_at_unix, used_at_unix, revoked_at_unix)
SELECT agent_id, issued_by, issued_at_unix, expires_at_unix, used_at_unix, revoked_at_unix FROM agent_certificate_recovery_grants;

DROP TABLE agent_certificate_recovery_grants;
ALTER TABLE agent_certificate_recovery_grants_new RENAME TO agent_certificate_recovery_grants;

COMMIT;

-- ─── discovered_clients ──────────────────────────────────────────────
-- Schema mirrors the post-0026 definition exactly; only the FK gains
-- ON DELETE CASCADE.
DELETE FROM discovered_clients WHERE agent_id NOT IN (SELECT id FROM agents);

BEGIN;

CREATE TABLE discovered_clients_new (
    id TEXT PRIMARY KEY,
    agent_id TEXT NOT NULL,
    client_name TEXT NOT NULL,
    secret TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'pending_review' CHECK (status IN ('pending_review','adopted','ignored')),
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
    FOREIGN KEY (agent_id) REFERENCES agents (id) ON DELETE CASCADE
);

INSERT INTO discovered_clients_new (
    id, agent_id, client_name, secret, status, total_octets, current_connections,
    active_unique_ips, connection_link, max_tcp_conns, max_unique_ips,
    data_quota_bytes, expiration, discovered_at_unix, updated_at_unix
)
SELECT id, agent_id, client_name, secret, status, total_octets, current_connections,
       active_unique_ips, connection_link, max_tcp_conns, max_unique_ips,
       data_quota_bytes, expiration, discovered_at_unix, updated_at_unix
FROM discovered_clients;

DROP TABLE discovered_clients;
ALTER TABLE discovered_clients_new RENAME TO discovered_clients;

CREATE INDEX IF NOT EXISTS idx_discovered_clients_agent_id ON discovered_clients (agent_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_discovered_clients_pending_unique
    ON discovered_clients (agent_id, client_name)
    WHERE status = 'pending_review';

COMMIT;

PRAGMA foreign_keys = ON;

-- +goose Down
-- Dev-stage: drop+recreate acceptable, no rollback.
SELECT 1;
