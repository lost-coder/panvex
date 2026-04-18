-- +goose Up
-- P2-DB-03 (DF-24 / M-F11): add missing foreign-key constraints and ON DELETE
-- CASCADE semantics so deleting a parent row (user, client) does not leave
-- orphan rows behind (sessions, enrollment tokens, metric snapshots, client
-- assignments, client deployments).
--
-- Dev-stage policy (plan v4): we only operate on dev databases. Drop-and-
-- recreate is acceptable, which means we clean up obvious orphan rows before
-- ADD CONSTRAINT so the migration does not abort on legacy data.

-- Sessions: 0004_sessions.sql created the table without a FK. Clean orphan
-- sessions, then attach the CASCADE FK to users(id).
DELETE FROM sessions
WHERE user_id NOT IN (SELECT id FROM users);

ALTER TABLE sessions
    DROP CONSTRAINT IF EXISTS fk_sessions_user_id;

ALTER TABLE sessions
    ADD CONSTRAINT fk_sessions_user_id
    FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE;

-- Enrollment tokens: deliberately NOT linked to fleet_groups by a FK. The
-- control-plane issues tokens against a fleet group id that may not yet exist
-- — agent_flow.consumeEnrollmentToken lazily creates the group on first
-- enrollment. Adding a FK here would break that "issue-then-create" flow.
-- See internal/controlplane/server/agent_flow.go (consumeEnrollmentToken)
-- and P2-DB-03 discussion.

-- Metric snapshots: PG 0001_init already has a FK on agent_id but without
-- CASCADE. Replace with a CASCADE variant so DeleteAgent cleans up history.
DELETE FROM metric_snapshots
WHERE agent_id NOT IN (SELECT id FROM agents);

ALTER TABLE metric_snapshots
    DROP CONSTRAINT IF EXISTS metric_snapshots_agent_id_fkey;
ALTER TABLE metric_snapshots
    DROP CONSTRAINT IF EXISTS fk_metric_snapshots_agent_id;

ALTER TABLE metric_snapshots
    ADD CONSTRAINT fk_metric_snapshots_agent_id
    FOREIGN KEY (agent_id) REFERENCES agents (id) ON DELETE CASCADE;

-- client_assignments.client_id already has ON DELETE CASCADE (from 0001_init).
-- The remaining FKs (fleet_group_id, agent_id) reference optional pointers and
-- should cascade-null when the target disappears.
DELETE FROM client_assignments
WHERE fleet_group_id IS NOT NULL
  AND fleet_group_id NOT IN (SELECT id FROM fleet_groups);

DELETE FROM client_assignments
WHERE agent_id IS NOT NULL
  AND agent_id NOT IN (SELECT id FROM agents);

ALTER TABLE client_assignments
    DROP CONSTRAINT IF EXISTS client_assignments_fleet_group_id_fkey;
ALTER TABLE client_assignments
    DROP CONSTRAINT IF EXISTS fk_client_assignments_fleet_group_id;

ALTER TABLE client_assignments
    ADD CONSTRAINT fk_client_assignments_fleet_group_id
    FOREIGN KEY (fleet_group_id) REFERENCES fleet_groups (id) ON DELETE SET NULL;

ALTER TABLE client_assignments
    DROP CONSTRAINT IF EXISTS client_assignments_agent_id_fkey;
ALTER TABLE client_assignments
    DROP CONSTRAINT IF EXISTS fk_client_assignments_agent_id;

ALTER TABLE client_assignments
    ADD CONSTRAINT fk_client_assignments_agent_id
    FOREIGN KEY (agent_id) REFERENCES agents (id) ON DELETE SET NULL;

-- client_deployments.client_id already has ON DELETE CASCADE. Upgrade
-- client_deployments.agent_id to CASCADE so removing an agent prunes its rows.
DELETE FROM client_deployments
WHERE agent_id NOT IN (SELECT id FROM agents);

ALTER TABLE client_deployments
    DROP CONSTRAINT IF EXISTS client_deployments_agent_id_fkey;
ALTER TABLE client_deployments
    DROP CONSTRAINT IF EXISTS fk_client_deployments_agent_id;

ALTER TABLE client_deployments
    ADD CONSTRAINT fk_client_deployments_agent_id
    FOREIGN KEY (agent_id) REFERENCES agents (id) ON DELETE CASCADE;

-- +goose Down
-- dev-stage: drop+recreate acceptable, no rollback.
SELECT 1;
