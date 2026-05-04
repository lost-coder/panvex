-- +goose Up
ALTER TABLE panel_settings ADD COLUMN geoip_json TEXT NOT NULL DEFAULT '';
ALTER TABLE panel_settings ADD COLUMN geoip_state_json TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE panel_settings DROP COLUMN geoip_state_json;
ALTER TABLE panel_settings DROP COLUMN geoip_json;
