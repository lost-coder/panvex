-- R-Q-03: retention settings ride alongside the panel_settings row in
-- the retention_json column (added by migration 0009). These two
-- queries are the read/write hot path used by GetRetentionSettings /
-- PutRetentionSettings.

-- name: GetRetentionJSON :one
SELECT retention_json FROM panel_settings WHERE scope = $1;

-- name: UpsertRetentionJSON :exec
INSERT INTO panel_settings (scope, http_public_url, grpc_public_endpoint, retention_json, updated_at)
VALUES ($1, '', '', $2, $3)
ON CONFLICT (scope) DO UPDATE
SET retention_json = EXCLUDED.retention_json,
    updated_at = EXCLUDED.updated_at;
