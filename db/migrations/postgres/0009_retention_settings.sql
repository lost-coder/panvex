-- +goose Up
-- P2-REL-03: persist retention settings across restarts.
-- Adds a retention_json column to panel_settings so the singleton
-- scope='panel' row also carries the operator-managed retention
-- configuration as an opaque JSON blob. Empty string means "not
-- yet set" and causes the control-plane to fall back to defaults.
ALTER TABLE panel_settings
    ADD COLUMN IF NOT EXISTS retention_json TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE panel_settings
    DROP COLUMN IF EXISTS retention_json;
