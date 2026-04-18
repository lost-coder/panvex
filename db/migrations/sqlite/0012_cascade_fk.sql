-- +goose Up
-- +goose NO TRANSACTION
-- P2-DB-03 (DF-24 / M-F11): SQLite does not support ALTER TABLE ADD
-- CONSTRAINT, so we rebuild each affected table with the correct FK set and
-- copy rows across.
--
-- Dev-stage policy (plan v4): operate on dev databases only. Drop-and-
-- recreate is acceptable, orphan rows are purged before the rebuild so the
-- new FK constraints do not abort the copy. No down.sql.
--
-- Goose runs the migration on a single connection, so PRAGMA foreign_keys
-- toggling is scoped to this rebuild and does not leak to other connections.

PRAGMA foreign_keys = OFF;

-- sessions: current columns per 0004_sessions.sql: (id, user_id, created_at_unix).
DELETE FROM sessions WHERE user_id NOT IN (SELECT id FROM users);

CREATE TABLE sessions_new (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL,
    created_at_unix INTEGER NOT NULL,
    FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE
);

INSERT INTO sessions_new (id, user_id, created_at_unix)
SELECT id, user_id, created_at_unix FROM sessions;

DROP TABLE sessions;
ALTER TABLE sessions_new RENAME TO sessions;

CREATE INDEX IF NOT EXISTS idx_sessions_user_id ON sessions (user_id);
CREATE INDEX IF NOT EXISTS idx_sessions_created_at_unix ON sessions (created_at_unix);

-- enrollment_tokens: deliberately NOT linked to fleet_groups by a FK — the
-- control-plane issues tokens against a fleet group id that may not yet
-- exist (agent_flow.consumeEnrollmentToken creates the group on first
-- enrollment). Adding a FK here would break the issue-then-create flow.

-- metric_snapshots: current columns per 0001_init.sql + 0011_column_drift.sql:
-- (id, agent_id, instance_id, captured_at_unix, "values"). The `values`
-- column was renamed from `values_json` in 0011, and since `values` is a
-- reserved keyword in SQLite it must be double-quoted.
DELETE FROM metric_snapshots WHERE agent_id NOT IN (SELECT id FROM agents);

CREATE TABLE metric_snapshots_new (
    id TEXT PRIMARY KEY,
    agent_id TEXT NOT NULL,
    instance_id TEXT NOT NULL DEFAULT '',
    captured_at_unix INTEGER NOT NULL,
    "values" TEXT NOT NULL,
    FOREIGN KEY (agent_id) REFERENCES agents (id) ON DELETE CASCADE
);

INSERT INTO metric_snapshots_new (id, agent_id, instance_id, captured_at_unix, "values")
SELECT id, agent_id, instance_id, captured_at_unix, "values" FROM metric_snapshots;

DROP TABLE metric_snapshots;
ALTER TABLE metric_snapshots_new RENAME TO metric_snapshots;

CREATE INDEX IF NOT EXISTS idx_metric_snapshots_captured_at ON metric_snapshots (captured_at_unix);
CREATE INDEX IF NOT EXISTS idx_metric_snapshots_agent_captured ON metric_snapshots (agent_id, captured_at_unix);

-- client_assignments: current columns per 0001_init.sql:
-- (id, client_id, target_type, fleet_group_id, agent_id, created_at_unix).
DELETE FROM client_assignments WHERE client_id NOT IN (SELECT id FROM clients);
DELETE FROM client_assignments
WHERE fleet_group_id IS NOT NULL
  AND fleet_group_id NOT IN (SELECT id FROM fleet_groups);
DELETE FROM client_assignments
WHERE agent_id IS NOT NULL
  AND agent_id NOT IN (SELECT id FROM agents);

CREATE TABLE client_assignments_new (
    id TEXT PRIMARY KEY,
    client_id TEXT NOT NULL,
    target_type TEXT NOT NULL,
    fleet_group_id TEXT,
    agent_id TEXT,
    created_at_unix INTEGER NOT NULL,
    FOREIGN KEY (client_id) REFERENCES clients (id) ON DELETE CASCADE,
    FOREIGN KEY (fleet_group_id) REFERENCES fleet_groups (id) ON DELETE SET NULL,
    FOREIGN KEY (agent_id) REFERENCES agents (id) ON DELETE SET NULL
);

INSERT INTO client_assignments_new (id, client_id, target_type, fleet_group_id, agent_id, created_at_unix)
SELECT id, client_id, target_type, fleet_group_id, agent_id, created_at_unix FROM client_assignments;

DROP TABLE client_assignments;
ALTER TABLE client_assignments_new RENAME TO client_assignments;

CREATE INDEX IF NOT EXISTS idx_client_assignments_client_id ON client_assignments (client_id);

-- client_deployments: current columns per 0001_init.sql:
-- (client_id, agent_id, desired_operation, status, last_error,
--  connection_link, last_applied_at_unix, updated_at_unix).
DELETE FROM client_deployments WHERE client_id NOT IN (SELECT id FROM clients);
DELETE FROM client_deployments WHERE agent_id NOT IN (SELECT id FROM agents);

CREATE TABLE client_deployments_new (
    client_id TEXT NOT NULL,
    agent_id TEXT NOT NULL,
    desired_operation TEXT NOT NULL,
    status TEXT NOT NULL,
    last_error TEXT NOT NULL DEFAULT '',
    connection_link TEXT NOT NULL DEFAULT '',
    last_applied_at_unix INTEGER,
    updated_at_unix INTEGER NOT NULL,
    PRIMARY KEY (client_id, agent_id),
    FOREIGN KEY (client_id) REFERENCES clients (id) ON DELETE CASCADE,
    FOREIGN KEY (agent_id) REFERENCES agents (id) ON DELETE CASCADE
);

INSERT INTO client_deployments_new (client_id, agent_id, desired_operation, status, last_error, connection_link, last_applied_at_unix, updated_at_unix)
SELECT client_id, agent_id, desired_operation, status, last_error, connection_link, last_applied_at_unix, updated_at_unix FROM client_deployments;

DROP TABLE client_deployments;
ALTER TABLE client_deployments_new RENAME TO client_deployments;

CREATE INDEX IF NOT EXISTS idx_client_deployments_client_id ON client_deployments (client_id);

PRAGMA foreign_keys = ON;

-- +goose Down
-- +goose NO TRANSACTION
-- dev-stage: drop+recreate acceptable, no rollback.
SELECT 1;
