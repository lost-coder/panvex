-- +goose Up
-- Adds geoip_json (operator-managed config) and geoip_state_json
-- (worker-managed runtime state — last check / etag / size / error)
-- to the singleton scope='panel' row. Mirrors the retention_json /
-- update_settings pattern.
ALTER TABLE panel_settings
    ADD COLUMN IF NOT EXISTS geoip_json TEXT NOT NULL DEFAULT '';
ALTER TABLE panel_settings
    ADD COLUMN IF NOT EXISTS geoip_state_json TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE panel_settings DROP COLUMN IF EXISTS geoip_state_json;
ALTER TABLE panel_settings DROP COLUMN IF EXISTS geoip_json;
