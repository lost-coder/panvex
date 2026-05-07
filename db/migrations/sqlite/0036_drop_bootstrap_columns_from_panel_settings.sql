-- +goose Up
-- 0035: drop bootstrap-only columns from panel_settings.
-- Project is pre-release; no compatibility shim.
-- These columns are now sourced from env/config.toml via the settings registry.

CREATE TABLE panel_settings_backup (
    scope                 TEXT PRIMARY KEY,
    http_public_url       TEXT NOT NULL DEFAULT '',
    grpc_public_endpoint  TEXT NOT NULL DEFAULT '',
    password_min_length   INTEGER NOT NULL DEFAULT 10,
    retention_json        TEXT NOT NULL DEFAULT '',
    geoip_json            TEXT NOT NULL DEFAULT '',
    geoip_state_json      TEXT NOT NULL DEFAULT '',
    updated_at_unix       INTEGER NOT NULL
);

INSERT INTO panel_settings_backup
    (scope, http_public_url, grpc_public_endpoint,
     password_min_length, retention_json, geoip_json,
     geoip_state_json, updated_at_unix)
SELECT scope, http_public_url, grpc_public_endpoint,
       password_min_length, retention_json, geoip_json,
       geoip_state_json, updated_at_unix
  FROM panel_settings;

DROP TABLE panel_settings;
ALTER TABLE panel_settings_backup RENAME TO panel_settings;

-- +goose Down
-- intentionally empty (pre-release, no compatibility shim)
