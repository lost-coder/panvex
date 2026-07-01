-- +goose Up
-- SQLite mirror of postgres/0053_config_apply_batches.sql. fleet_group_id
-- stays TEXT (not UUID) since fleet_groups.id is TEXT on SQLite — see
-- sqlite/0014_fleet_groups_redesign.sql's header note; UUID format is
-- enforced at the application layer, matching the
-- user_fleet_group_scopes / client_assignments convention. Timestamps use
-- the project's `*_at_unix` INTEGER convention (see jobs/metric_snapshots
-- in sqlite/0001_init.sql) rather than TIMESTAMPTZ.
CREATE TABLE IF NOT EXISTS config_apply_batches (
    id                 TEXT PRIMARY KEY,
    fleet_group_id     TEXT NOT NULL REFERENCES fleet_groups (id) ON DELETE CASCADE,
    mode               TEXT NOT NULL CHECK (mode IN ('all_at_once', 'rolling')),
    wave_size          INTEGER NOT NULL DEFAULT 1,
    expected_revision  TEXT NOT NULL DEFAULT '',
    status             TEXT NOT NULL CHECK (status IN ('running', 'succeeded', 'failed', 'halted')),
    created_at_unix    INTEGER NOT NULL,
    updated_at_unix    INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_config_apply_batches_status
    ON config_apply_batches (status);

CREATE TABLE IF NOT EXISTS config_apply_batch_targets (
    batch_id    TEXT NOT NULL REFERENCES config_apply_batches (id) ON DELETE CASCADE,
    agent_id    TEXT NOT NULL,
    wave_index  INTEGER NOT NULL,
    job_id      TEXT NOT NULL DEFAULT '',
    status      TEXT NOT NULL CHECK (status IN ('pending', 'running', 'succeeded', 'failed', 'skipped')),
    PRIMARY KEY (batch_id, agent_id)
);

CREATE INDEX IF NOT EXISTS idx_config_apply_batch_targets_batch_wave
    ON config_apply_batch_targets (batch_id, wave_index);

-- +goose Down
DROP TABLE IF EXISTS config_apply_batch_targets;
DROP TABLE IF EXISTS config_apply_batches;
