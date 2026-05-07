-- +goose Up
-- 0036: drop bootstrap-only columns from panel_settings.
-- Project is pre-release; no compatibility shim.

ALTER TABLE panel_settings DROP COLUMN IF EXISTS http_listen_address;
ALTER TABLE panel_settings DROP COLUMN IF EXISTS grpc_listen_address;
ALTER TABLE panel_settings DROP COLUMN IF EXISTS tls_mode;
ALTER TABLE panel_settings DROP COLUMN IF EXISTS tls_cert_file;
ALTER TABLE panel_settings DROP COLUMN IF EXISTS tls_key_file;
ALTER TABLE panel_settings DROP COLUMN IF EXISTS http_root_path;

-- +goose Down
-- intentionally empty (pre-release, no compatibility shim)
