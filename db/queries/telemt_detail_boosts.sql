-- R-Q-03: telemt_detail_boosts — short-lived flag that the panel
-- raises so an agent emits high-frequency telemetry while the
-- detail page is open.

-- name: GetTelemetryDetailBoost :one
SELECT agent_id, expires_at, updated_at
FROM telemt_detail_boosts
WHERE agent_id = $1;

-- name: ListTelemetryDetailBoosts :many
SELECT agent_id, expires_at, updated_at
FROM telemt_detail_boosts
ORDER BY agent_id ASC;


-- name: UpsertTelemetryDetailBoost :exec
INSERT INTO telemt_detail_boosts (agent_id, expires_at, updated_at)
VALUES ($1, $2, $3)
ON CONFLICT (agent_id) DO UPDATE
SET expires_at = EXCLUDED.expires_at,
    updated_at = EXCLUDED.updated_at;

-- name: DeleteTelemetryDetailBoost :execrows
DELETE FROM telemt_detail_boosts WHERE agent_id = $1;

-- name: PruneExpiredTelemetryDetailBoosts :execrows
DELETE FROM telemt_detail_boosts WHERE expires_at < $1;
