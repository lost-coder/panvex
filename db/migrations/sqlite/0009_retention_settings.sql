-- +goose Up
-- P2-REL-03: persist retention settings across restarts.
-- Adds a retention_json column to panel_settings so the singleton
-- scope='panel' row also carries the operator-managed retention
-- configuration as an opaque JSON blob. Empty string means "not
-- yet set" and causes the control-plane to fall back to defaults.
-- SQLite's ALTER TABLE ADD COLUMN lacks IF NOT EXISTS; goose records
-- this version so it never runs twice on the same database.
ALTER TABLE panel_settings
    ADD COLUMN retention_json TEXT NOT NULL DEFAULT '';

-- +goose Down
-- SQLite cannot DROP COLUMN on older versions; recreate the table
-- without retention_json to roll back. This Down path is used only
-- by tests / operator-initiated downgrades — production drops table
-- contents via 0001's Down.
CREATE TABLE panel_settings_backup (
    scope TEXT PRIMARY KEY,
    http_public_url TEXT NOT NULL DEFAULT '',
    http_root_path TEXT NOT NULL DEFAULT '',
    grpc_public_endpoint TEXT NOT NULL DEFAULT '',
    http_listen_address TEXT NOT NULL DEFAULT '',
    grpc_listen_address TEXT NOT NULL DEFAULT '',
    tls_mode TEXT NOT NULL DEFAULT '',
    tls_cert_file TEXT NOT NULL DEFAULT '',
    tls_key_file TEXT NOT NULL DEFAULT '',
    updated_at_unix INTEGER NOT NULL
);
INSERT INTO panel_settings_backup
    SELECT scope, http_public_url, http_root_path, grpc_public_endpoint,
           http_listen_address, grpc_listen_address, tls_mode, tls_cert_file,
           tls_key_file, updated_at_unix
    FROM panel_settings;
DROP TABLE panel_settings;
ALTER TABLE panel_settings_backup RENAME TO panel_settings;
