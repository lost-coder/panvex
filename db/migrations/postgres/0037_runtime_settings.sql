-- +goose Up
-- 0037: kv-table for operational settings without dedicated columns.
CREATE TABLE runtime_settings (
    name        TEXT PRIMARY KEY,
    value_json  TEXT NOT NULL,
    updated_at  BIGINT NOT NULL,
    updated_by  TEXT NOT NULL DEFAULT ''
);

-- +goose Down
DROP TABLE runtime_settings;
