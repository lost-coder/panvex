-- runtime_settings: operational settings without a dedicated typed
-- column on panel_settings. Backing store for sub-project B fields.

-- name: GetRuntimeSetting :one
SELECT name, value_json, updated_at, updated_by
  FROM runtime_settings
 WHERE name = $1;

-- name: UpsertRuntimeSetting :exec
INSERT INTO runtime_settings (name, value_json, updated_at, updated_by)
VALUES ($1, $2, $3, $4)
ON CONFLICT (name) DO UPDATE
   SET value_json = EXCLUDED.value_json,
       updated_at = EXCLUDED.updated_at,
       updated_by = EXCLUDED.updated_by;
