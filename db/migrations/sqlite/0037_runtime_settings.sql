-- +goose Up
-- 0037: kv-table for operational settings without dedicated columns.
-- Used for fields produced by sub-project B (poll intervals, etc.).
CREATE TABLE runtime_settings (
    name        TEXT PRIMARY KEY,
    value_json  TEXT NOT NULL,
    updated_at  INTEGER NOT NULL,
    updated_by  TEXT NOT NULL DEFAULT ''
);

-- +goose Down
DROP TABLE runtime_settings;
