-- +goose Up
-- config_apply_batches coordinates one group-wide config-apply rollout: an
-- operator triggers a config push to every agent in a fleet group, and the
-- batch tracks delivery across one or more waves depending on mode
-- (all_at_once vs rolling). expected_revision pins the agent_config_targets
-- revision this batch is rolling out so a concurrent edit to the group's
-- desired config cannot silently change what an in-flight batch delivers.
--
-- fleet_group_id is UUID (not TEXT) to match fleet_groups.id's type on
-- PostgreSQL (postgres/0014_fleet_groups_redesign.sql) — PostgreSQL refuses
-- to create a FOREIGN KEY between mismatched column types. See
-- sqlite/0053_config_apply_batches.sql for the TEXT-typed mirror (SQLite
-- kept fleet_groups.id as TEXT — see sqlite/0014's header note).
CREATE TABLE IF NOT EXISTS config_apply_batches (
    id                 TEXT PRIMARY KEY,
    fleet_group_id     UUID NOT NULL REFERENCES fleet_groups (id) ON DELETE CASCADE,
    mode               TEXT NOT NULL CHECK (mode IN ('all_at_once', 'rolling')),
    wave_size          INT NOT NULL DEFAULT 1,
    expected_revision  TEXT NOT NULL DEFAULT '',
    status             TEXT NOT NULL CHECK (status IN ('running', 'succeeded', 'failed', 'halted')),
    created_at         TIMESTAMPTZ NOT NULL,
    updated_at         TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_config_apply_batches_status
    ON config_apply_batches (status);

-- config_apply_batch_targets is one agent's delivery record within a batch.
-- job_id is '' until the target's wave is enqueued.
CREATE TABLE IF NOT EXISTS config_apply_batch_targets (
    batch_id    TEXT NOT NULL REFERENCES config_apply_batches (id) ON DELETE CASCADE,
    agent_id    TEXT NOT NULL,
    wave_index  INT NOT NULL,
    job_id      TEXT NOT NULL DEFAULT '',
    status      TEXT NOT NULL CHECK (status IN ('pending', 'running', 'succeeded', 'failed', 'skipped')),
    PRIMARY KEY (batch_id, agent_id)
);

CREATE INDEX IF NOT EXISTS idx_config_apply_batch_targets_batch_wave
    ON config_apply_batch_targets (batch_id, wave_index);

-- +goose Down
DROP TABLE IF EXISTS config_apply_batch_targets;
DROP TABLE IF EXISTS config_apply_batches;
