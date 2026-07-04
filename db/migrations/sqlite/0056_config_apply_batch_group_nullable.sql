-- +goose Up
-- P3-3.4: зеркало postgres/0056 — fleet_group_id становится nullable
-- (NULL = agent-scoped batch-of-one). SQLite не умеет ALTER COLUMN DROP
-- NOT NULL — пересборка таблицы (приём как в 0050_check_parity.sql).
PRAGMA foreign_keys=OFF;

CREATE TABLE config_apply_batches_new (
    id                 TEXT PRIMARY KEY,
    fleet_group_id     TEXT REFERENCES fleet_groups (id) ON DELETE CASCADE,
    mode               TEXT NOT NULL CHECK (mode IN ('all_at_once', 'rolling')),
    wave_size          INTEGER NOT NULL DEFAULT 1,
    expected_revision  TEXT NOT NULL DEFAULT '',
    status             TEXT NOT NULL CHECK (status IN ('running', 'succeeded', 'failed', 'halted')),
    created_at_unix    INTEGER NOT NULL,
    updated_at_unix    INTEGER NOT NULL
);
INSERT INTO config_apply_batches_new
    (id, fleet_group_id, mode, wave_size, expected_revision, status, created_at_unix, updated_at_unix)
SELECT id, fleet_group_id, mode, wave_size, expected_revision, status, created_at_unix, updated_at_unix
FROM config_apply_batches;
DROP TABLE config_apply_batches;
ALTER TABLE config_apply_batches_new RENAME TO config_apply_batches;

CREATE INDEX IF NOT EXISTS idx_config_apply_batches_status
    ON config_apply_batches (status);

PRAGMA foreign_keys=ON;

-- +goose Down
-- Dev-stage: drop+recreate acceptable, no rollback.
SELECT 1;
