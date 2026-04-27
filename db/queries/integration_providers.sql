-- R-Q-03: integration_providers — operator-managed credential
-- providers (api keys / webhooks for fleet group integrations).

-- name: GetIntegrationProvider :one
SELECT id, kind, label, config, created_at, updated_at
FROM integration_providers
WHERE id = $1;

-- name: ListIntegrationProviders :many
SELECT id, kind, label, config, created_at, updated_at
FROM integration_providers
ORDER BY label ASC, id ASC;


-- name: UpsertIntegrationProvider :exec
INSERT INTO integration_providers (id, kind, label, config, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (id) DO UPDATE
SET kind       = EXCLUDED.kind,
    label      = EXCLUDED.label,
    config     = EXCLUDED.config,
    updated_at = EXCLUDED.updated_at;

-- name: DeleteIntegrationProvider :execrows
DELETE FROM integration_providers WHERE id = $1;
