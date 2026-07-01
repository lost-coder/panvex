-- +goose Up
-- +goose NO TRANSACTION
-- M3: On PostgreSQL, JSON-shaped columns are typed JSONB and the server
-- rejects malformed JSON at write time. On SQLite these columns are plain
-- TEXT with zero validation — malformed JSON is silently accepted, which
-- diverges write-acceptance behaviour from PostgreSQL and would let a
-- corrupt row survive the offline SQLite→PostgreSQL migration only to
-- fail there instead of at the original write. SQLite has no JSONB type,
-- so the column stays TEXT; a `json_valid(...)` CHECK constraint is the
-- compensating control.
--
-- SQLite has no ALTER TABLE ADD CONSTRAINT, so each affected table is
-- rebuilt with the create/copy/drop/rename/index recipe established in
-- 0026/0048/92b357e5 (M5): every DROP TABLE / RENAME pair is wrapped in
-- its own explicit BEGIN/COMMIT so a crash between them can never leave a
-- table dropped-but-not-renamed. PRAGMA foreign_keys stays outside every
-- BEGIN/COMMIT — SQLite forbids toggling it inside a transaction — which
-- is also why this whole file runs under NO TRANSACTION.
--
-- Nine columns across eight tables are covered. Per-column CHECK/default
-- decision (all chosen so NO existing valid row can be rejected by the
-- new CHECK):
--
--   jobs.payload_json            — default stays '' (permissive CHECK:
--       `payload_json = '' OR json_valid(payload_json)`). Unlike the
--       other columns, '' is a real, exercised sentinel meaning "no
--       payload" (see jobs.Repository's runPutNilPayload contract test
--       and PutJob call sites that persist a job before its payload is
--       known) — rewriting the default to '{}' would not by itself stop
--       callers from continuing to write '' explicitly, so the permissive
--       form is the only option that does not reject live traffic.
--   audit_events.details         — already defaults to '{}' (valid JSON,
--       object shape); plain `CHECK (json_valid(details))`.
--   metric_snapshots."values"    — NOT NULL, no default; every write goes
--       through encodeJSON(map[string]uint64), always valid JSON; plain
--       `CHECK (json_valid("values"))`.
--   integration_providers.config — already defaults to '{}' (object
--       shape) but is NOT always plain JSON: fleet.Service.
--       encryptProviderConfig seals it under the vault's
--       "integration_config" domain when a vault is configured, storing
--       a "PVS1:"/"PVS2:"/"PVS3:" prefixed ciphertext string instead
--       (internal/controlplane/secretvault). A plain json_valid CHECK
--       would reject every encrypted row, so this column gets the
--       permissive form: `CHECK (json_valid(config) OR config LIKE
--       'PVS_:%')` — the single-char wildcard covers all three prefix
--       generations without hardcoding each one.
--   fleet_group_integrations.config — already defaults to '{}' (object
--       shape); plain `CHECK (json_valid(config))`.
--   client_deployments.connection_links — already defaults to '[]'
--       (array shape); every write goes through encodeStringArray, always
--       valid JSON; plain `CHECK (json_valid(connection_links))`.
--   discovered_clients.connection_links — same as above; plain
--       `CHECK (json_valid(connection_links))`.
--   enrollment_events.fields_json — JSONB and nullable on PostgreSQL
--       (db/migrations/postgres/0041_enrollment_attempts.sql); no default
--       on either backend. enrollment.Recorder.Event/Ingest
--       (internal/controlplane/enrollment/recorder.go) only ever calls
--       json.Marshal(fields) when len(fields) > 0 and otherwise leaves
--       FieldsJSON == ""; SQLStore.AppendEvent
--       (internal/controlplane/enrollment/sqlstore.go) maps that empty
--       string to a NULL bind param, never an empty-string column value.
--       So the column is either NULL or valid JSON, never "". CHECK must
--       explicitly admit NULL: `CHECK (fields_json IS NULL OR
--       json_valid(fields_json))` — SQLite's json_valid(NULL) already
--       evaluates to NULL, which a CHECK treats as passing, but the
--       explicit IS NULL keeps the constraint's intent unambiguous.
--   webhook_outbox.payload — JSONB and NOT NULL on PostgreSQL
--       (db/migrations/postgres/0039_webhook_outbox.sql); no default on
--       either backend. The only writer, WebhookStore.InsertOutbox
--       (internal/controlplane/storage/sqlite/webhooks.go), substitutes
--       the literal `{}` whenever the caller-supplied payload is empty,
--       so every persisted value is valid JSON; plain
--       `CHECK (json_valid(payload))`.
--
-- Every rebuilt table's INSERT..SELECT is a straight column-for-column
-- copy (no backfill needed): every existing default above is already
-- valid JSON, so no live row can trip its column's new CHECK.

PRAGMA foreign_keys = OFF;

-- ─── jobs ────────────────────────────────────────────────────────────
BEGIN;

CREATE TABLE jobs_new (
    id TEXT PRIMARY KEY,
    action TEXT NOT NULL,
    actor_id TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('queued','running','succeeded','failed','expired','partial')),
    created_at_unix INTEGER NOT NULL,
    ttl_nanos INTEGER NOT NULL,
    idempotency_key TEXT NOT NULL UNIQUE,
    payload_json TEXT NOT NULL DEFAULT ''
        CHECK (payload_json = '' OR json_valid(payload_json))
);

INSERT INTO jobs_new (id, action, actor_id, status, created_at_unix, ttl_nanos, idempotency_key, payload_json)
SELECT id, action, actor_id, status, created_at_unix, ttl_nanos, idempotency_key, payload_json FROM jobs;

DROP TABLE jobs;
ALTER TABLE jobs_new RENAME TO jobs;

CREATE INDEX IF NOT EXISTS idx_jobs_created_at ON jobs (created_at_unix);
CREATE INDEX IF NOT EXISTS idx_jobs_status ON jobs (status);
CREATE INDEX IF NOT EXISTS idx_jobs_actor_id ON jobs (actor_id);

COMMIT;

-- ─── audit_events ────────────────────────────────────────────────────
BEGIN;

CREATE TABLE audit_events_new (
    id TEXT PRIMARY KEY,
    actor_id TEXT NOT NULL,
    action TEXT NOT NULL,
    target_id TEXT NOT NULL,
    created_at_unix INTEGER NOT NULL,
    details TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(details)),
    prev_hash TEXT NOT NULL DEFAULT '',
    event_hash TEXT NOT NULL DEFAULT ''
);

INSERT INTO audit_events_new (id, actor_id, action, target_id, created_at_unix, details, prev_hash, event_hash)
SELECT id, actor_id, action, target_id, created_at_unix, details, prev_hash, event_hash FROM audit_events;

DROP TABLE audit_events;
ALTER TABLE audit_events_new RENAME TO audit_events;

CREATE INDEX IF NOT EXISTS idx_audit_events_chain_walk ON audit_events (created_at_unix, id);
CREATE INDEX IF NOT EXISTS idx_audit_events_created_at ON audit_events (created_at_unix);

COMMIT;

-- ─── metric_snapshots ────────────────────────────────────────────────
-- `values` is a reserved keyword in SQLite; the identifier stays quoted.
BEGIN;

CREATE TABLE metric_snapshots_new (
    id TEXT PRIMARY KEY,
    agent_id TEXT NOT NULL,
    instance_id TEXT NOT NULL DEFAULT '',
    captured_at_unix INTEGER NOT NULL,
    "values" TEXT NOT NULL CHECK (json_valid("values")),
    FOREIGN KEY (agent_id) REFERENCES agents (id) ON DELETE CASCADE
);

INSERT INTO metric_snapshots_new (id, agent_id, instance_id, captured_at_unix, "values")
SELECT id, agent_id, instance_id, captured_at_unix, "values" FROM metric_snapshots;

DROP TABLE metric_snapshots;
ALTER TABLE metric_snapshots_new RENAME TO metric_snapshots;

CREATE INDEX IF NOT EXISTS idx_metric_snapshots_agent_captured ON metric_snapshots (agent_id, captured_at_unix);
CREATE INDEX IF NOT EXISTS idx_metric_snapshots_captured_at ON metric_snapshots (captured_at_unix);

COMMIT;

-- ─── integration_providers ───────────────────────────────────────────
BEGIN;

CREATE TABLE integration_providers_new (
    id              TEXT PRIMARY KEY,
    kind            TEXT NOT NULL,
    label           TEXT NOT NULL DEFAULT '',
    -- Permissive: config is either plain JSON or a vault-sealed
    -- "PVS1:"/"PVS2:"/"PVS3:" ciphertext string (see file header note).
    config          TEXT NOT NULL DEFAULT '{}'
        CHECK (json_valid(config) OR config LIKE 'PVS_:%'),
    created_at_unix INTEGER NOT NULL,
    updated_at_unix INTEGER NOT NULL
);

INSERT INTO integration_providers_new (id, kind, label, config, created_at_unix, updated_at_unix)
SELECT id, kind, label, config, created_at_unix, updated_at_unix FROM integration_providers;

DROP TABLE integration_providers;
ALTER TABLE integration_providers_new RENAME TO integration_providers;

CREATE INDEX IF NOT EXISTS idx_integration_providers_kind ON integration_providers (kind);

COMMIT;

-- ─── fleet_group_integrations ────────────────────────────────────────
BEGIN;

CREATE TABLE fleet_group_integrations_new (
    id              TEXT PRIMARY KEY,
    fleet_group_id  TEXT NOT NULL,
    kind            TEXT NOT NULL,
    provider_id     TEXT,
    config          TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(config)),
    enabled         INTEGER NOT NULL DEFAULT 0,
    created_at_unix INTEGER NOT NULL,
    updated_at_unix INTEGER NOT NULL,
    FOREIGN KEY (fleet_group_id) REFERENCES fleet_groups (id) ON DELETE CASCADE,
    FOREIGN KEY (provider_id)    REFERENCES integration_providers (id) ON DELETE SET NULL,
    UNIQUE (fleet_group_id, kind)
);

INSERT INTO fleet_group_integrations_new (id, fleet_group_id, kind, provider_id, config, enabled, created_at_unix, updated_at_unix)
SELECT id, fleet_group_id, kind, provider_id, config, enabled, created_at_unix, updated_at_unix FROM fleet_group_integrations;

DROP TABLE fleet_group_integrations;
ALTER TABLE fleet_group_integrations_new RENAME TO fleet_group_integrations;

CREATE INDEX IF NOT EXISTS idx_fleet_group_integrations_fleet_group_id ON fleet_group_integrations (fleet_group_id);
CREATE INDEX IF NOT EXISTS idx_fleet_group_integrations_kind ON fleet_group_integrations (kind);

COMMIT;

-- ─── client_deployments ──────────────────────────────────────────────
BEGIN;

CREATE TABLE client_deployments_new (
    client_id TEXT NOT NULL,
    agent_id TEXT NOT NULL,
    desired_operation TEXT NOT NULL,
    status TEXT NOT NULL,
    last_error TEXT NOT NULL DEFAULT '',
    last_applied_at_unix INTEGER,
    updated_at_unix INTEGER NOT NULL,
    connection_links TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(connection_links)),
    last_reset_epoch_secs INTEGER NOT NULL DEFAULT 0,
    link_diagnostic TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (client_id, agent_id),
    FOREIGN KEY (client_id) REFERENCES clients (id) ON DELETE CASCADE,
    FOREIGN KEY (agent_id) REFERENCES agents (id) ON DELETE CASCADE
);

INSERT INTO client_deployments_new (
    client_id, agent_id, desired_operation, status, last_error, last_applied_at_unix,
    updated_at_unix, connection_links, last_reset_epoch_secs, link_diagnostic
)
SELECT client_id, agent_id, desired_operation, status, last_error, last_applied_at_unix,
       updated_at_unix, connection_links, last_reset_epoch_secs, link_diagnostic
FROM client_deployments;

DROP TABLE client_deployments;
ALTER TABLE client_deployments_new RENAME TO client_deployments;

CREATE INDEX IF NOT EXISTS idx_client_deployments_client_id ON client_deployments (client_id);

COMMIT;

-- ─── discovered_clients ──────────────────────────────────────────────
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
    max_tcp_conns INTEGER NOT NULL DEFAULT 0,
    max_unique_ips INTEGER NOT NULL DEFAULT 0,
    data_quota_bytes INTEGER NOT NULL DEFAULT 0,
    expiration TEXT NOT NULL DEFAULT '',
    discovered_at_unix INTEGER NOT NULL,
    updated_at_unix INTEGER NOT NULL,
    connection_links TEXT NOT NULL DEFAULT '[]' CHECK (json_valid(connection_links)),
    UNIQUE (agent_id, client_name),
    FOREIGN KEY (agent_id) REFERENCES agents (id) ON DELETE CASCADE
);

INSERT INTO discovered_clients_new (
    id, agent_id, client_name, secret, status, total_octets, current_connections,
    active_unique_ips, max_tcp_conns, max_unique_ips, data_quota_bytes, expiration,
    discovered_at_unix, updated_at_unix, connection_links
)
SELECT id, agent_id, client_name, secret, status, total_octets, current_connections,
       active_unique_ips, max_tcp_conns, max_unique_ips, data_quota_bytes, expiration,
       discovered_at_unix, updated_at_unix, connection_links
FROM discovered_clients;

DROP TABLE discovered_clients;
ALTER TABLE discovered_clients_new RENAME TO discovered_clients;

CREATE INDEX IF NOT EXISTS idx_discovered_clients_agent_id ON discovered_clients (agent_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_discovered_clients_pending_unique
    ON discovered_clients (agent_id, client_name)
    WHERE status = 'pending_review';

COMMIT;

-- ─── enrollment_events ───────────────────────────────────────────────
BEGIN;

CREATE TABLE enrollment_events_new (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    attempt_id  TEXT NOT NULL REFERENCES enrollment_attempts (id) ON DELETE CASCADE,
    ts          TIMESTAMP NOT NULL,
    step        TEXT NOT NULL,
    level       TEXT NOT NULL CHECK (level IN ('info', 'warn', 'error')),
    message     TEXT,
    fields_json TEXT CHECK (fields_json IS NULL OR json_valid(fields_json))
);

INSERT INTO enrollment_events_new (id, attempt_id, ts, step, level, message, fields_json)
SELECT id, attempt_id, ts, step, level, message, fields_json FROM enrollment_events;

DROP TABLE enrollment_events;
ALTER TABLE enrollment_events_new RENAME TO enrollment_events;

CREATE INDEX IF NOT EXISTS idx_enrollment_events_attempt ON enrollment_events (attempt_id, ts);

COMMIT;

-- ─── webhook_outbox ──────────────────────────────────────────────────
BEGIN;

CREATE TABLE webhook_outbox_new (
    id              TEXT PRIMARY KEY,
    endpoint_id     TEXT NOT NULL REFERENCES webhook_endpoints (id) ON DELETE CASCADE,
    event_action    TEXT NOT NULL,
    payload         TEXT NOT NULL CHECK (json_valid(payload)),
    attempt         INTEGER NOT NULL DEFAULT 0,
    next_attempt_at TIMESTAMP NOT NULL,
    last_error      TEXT NOT NULL DEFAULT '',
    dead            INTEGER NOT NULL DEFAULT 0 CHECK (dead IN (0, 1)),
    created_at      TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    delivered_at    TIMESTAMP
);

INSERT INTO webhook_outbox_new (
    id, endpoint_id, event_action, payload, attempt, next_attempt_at,
    last_error, dead, created_at, delivered_at
)
SELECT id, endpoint_id, event_action, payload, attempt, next_attempt_at,
       last_error, dead, created_at, delivered_at
FROM webhook_outbox;

DROP TABLE webhook_outbox;
ALTER TABLE webhook_outbox_new RENAME TO webhook_outbox;

CREATE INDEX IF NOT EXISTS idx_webhook_outbox_ready
    ON webhook_outbox (next_attempt_at)
    WHERE dead = 0 AND delivered_at IS NULL;

COMMIT;

PRAGMA foreign_keys = ON;

-- +goose Down
SELECT 1;
