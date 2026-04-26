-- R-Q-03: fleet_group_integrations — per-fleet-group provider hookup.

-- name: GetFleetGroupIntegration :one
SELECT id, fleet_group_id, kind, provider_id, config, enabled,
       created_at, updated_at
FROM fleet_group_integrations
WHERE id = $1;

-- name: ListFleetGroupIntegrations :many
SELECT id, fleet_group_id, kind, provider_id, config, enabled,
       created_at, updated_at
FROM fleet_group_integrations
WHERE fleet_group_id = $1
ORDER BY created_at, id;

-- name: UpsertFleetGroupIntegration :exec
INSERT INTO fleet_group_integrations (id, fleet_group_id, kind, provider_id,
                                      config, enabled, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT (id) DO UPDATE
SET kind        = EXCLUDED.kind,
    provider_id = EXCLUDED.provider_id,
    config      = EXCLUDED.config,
    enabled     = EXCLUDED.enabled,
    updated_at  = EXCLUDED.updated_at;

-- name: DeleteFleetGroupIntegration :execrows
DELETE FROM fleet_group_integrations WHERE id = $1;
