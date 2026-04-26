-- +goose Up
-- +goose NO TRANSACTION
-- R-D-02-sqlite: bring SQLite into parity with postgres migration 0023
-- by rebuilding jobs / job_targets / discovered_clients with explicit
-- CHECK constraints on their enum-shaped TEXT columns. SQLite does not
-- support ALTER TABLE ADD CONSTRAINT, so the rebuild pattern from
-- 0012_cascade_fk is reused: rename → recreate → copy → drop → restore.
--
-- Dev-stage policy: drop-and-recreate is acceptable; rows that fail the
-- new CHECK are purged before the copy so the new constraint does not
-- abort the migration on dirty data.

PRAGMA foreign_keys = OFF;

-- ─── jobs ────────────────────────────────────────────────────────────
-- Status enum mirrors jobs.IsValidStatus in the Go layer.
DELETE FROM jobs WHERE status NOT IN ('queued','running','succeeded','failed','expired');

CREATE TABLE jobs_new (
    id TEXT PRIMARY KEY,
    action TEXT NOT NULL,
    actor_id TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('queued','running','succeeded','failed','expired')),
    created_at_unix INTEGER NOT NULL,
    ttl_nanos INTEGER NOT NULL,
    idempotency_key TEXT NOT NULL UNIQUE,
    payload_json TEXT NOT NULL DEFAULT ''
);

INSERT INTO jobs_new (id, action, actor_id, status, created_at_unix, ttl_nanos, idempotency_key, payload_json)
SELECT id, action, actor_id, status, created_at_unix, ttl_nanos, idempotency_key, payload_json FROM jobs;

DROP TABLE jobs;
ALTER TABLE jobs_new RENAME TO jobs;

CREATE INDEX IF NOT EXISTS idx_jobs_created_at ON jobs (created_at_unix);
CREATE INDEX IF NOT EXISTS idx_jobs_status ON jobs (status);
CREATE INDEX IF NOT EXISTS idx_jobs_actor_id ON jobs (actor_id);

-- ─── job_targets ─────────────────────────────────────────────────────
DELETE FROM job_targets WHERE status NOT IN ('queued','dispatched','acknowledged','succeeded','failed','expired');

CREATE TABLE job_targets_new (
    job_id TEXT NOT NULL,
    agent_id TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('queued','dispatched','acknowledged','succeeded','failed','expired')),
    result_text TEXT NOT NULL DEFAULT '',
    result_json TEXT NOT NULL DEFAULT '',
    updated_at_unix INTEGER NOT NULL,
    PRIMARY KEY (job_id, agent_id),
    FOREIGN KEY (job_id) REFERENCES jobs (id)
);

INSERT INTO job_targets_new (job_id, agent_id, status, result_text, result_json, updated_at_unix)
SELECT job_id, agent_id, status, result_text, result_json, updated_at_unix FROM job_targets;

DROP TABLE job_targets;
ALTER TABLE job_targets_new RENAME TO job_targets;

CREATE INDEX IF NOT EXISTS idx_job_targets_agent_id ON job_targets (agent_id);

-- ─── discovered_clients ──────────────────────────────────────────────
DELETE FROM discovered_clients WHERE status NOT IN ('pending_review','adopted','ignored');

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
    FOREIGN KEY (agent_id) REFERENCES agents (id)
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

PRAGMA foreign_keys = ON;

-- +goose Down
SELECT 1;
