-- +goose Up
-- P3-3.1 (аудит #3): см. sqlite/0055_runtime_current_json.sql. Колонки и
-- типы зеркалят SQLite по конвенции *_at_unix ↔ TIMESTAMPTZ
-- (schema_sync_test нормализует суффиксы _unix/_at).
DROP TABLE telemt_runtime_current;
CREATE TABLE telemt_runtime_current (
    agent_id TEXT PRIMARY KEY REFERENCES agents (id) ON DELETE CASCADE,
    observed_at TIMESTAMPTZ NOT NULL,
    runtime_json TEXT NOT NULL DEFAULT ''
);

-- +goose Down
SELECT 1;
