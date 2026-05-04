-- GeoIP settings (operator-managed) and state (worker-managed) ride
-- alongside the panel_settings singleton row in the geoip_json and
-- geoip_state_json columns added by migration 0034. Both are treated
-- as opaque JSON blobs at the storage layer; schema validation is the
-- server's job. Mirrors the retention_settings pattern.

-- name: GetGeoIPJSON :one
SELECT geoip_json FROM panel_settings WHERE scope = $1;

-- name: UpsertGeoIPJSON :exec
INSERT INTO panel_settings (scope, http_public_url, grpc_public_endpoint, geoip_json, updated_at)
VALUES ($1, '', '', $2, $3)
ON CONFLICT (scope) DO UPDATE
SET geoip_json = EXCLUDED.geoip_json,
    updated_at = EXCLUDED.updated_at;

-- name: GetGeoIPStateJSON :one
SELECT geoip_state_json FROM panel_settings WHERE scope = $1;

-- name: UpsertGeoIPStateJSON :exec
INSERT INTO panel_settings (scope, http_public_url, grpc_public_endpoint, geoip_state_json, updated_at)
VALUES ($1, '', '', $2, $3)
ON CONFLICT (scope) DO UPDATE
SET geoip_state_json = EXCLUDED.geoip_state_json,
    updated_at = EXCLUDED.updated_at;
