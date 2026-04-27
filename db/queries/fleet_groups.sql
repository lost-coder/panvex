-- R-Q-03: fleet_groups — operator-managed grouping of agents +
-- enrollment-tokens + client-assignments. Note: id is UUID on
-- postgres (since migration 0014).

-- name: GetFleetGroup :one
SELECT id, name, label, description, created_at, updated_at
FROM fleet_groups
WHERE id = $1;

-- name: GetFleetGroupByName :one
SELECT id, name, label, description, created_at, updated_at
FROM fleet_groups
WHERE name = $1;

-- name: ListFleetGroups :many
SELECT id, name, label, description, created_at, updated_at
FROM fleet_groups
ORDER BY created_at ASC, id ASC;


-- name: UpsertFleetGroup :exec
INSERT INTO fleet_groups (id, name, label, description, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (id) DO UPDATE
SET name        = EXCLUDED.name,
    label       = EXCLUDED.label,
    description = EXCLUDED.description,
    created_at  = EXCLUDED.created_at,
    updated_at  = EXCLUDED.updated_at;

-- name: CreateFleetGroup :exec
INSERT INTO fleet_groups (id, name, label, description, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6);

-- name: UpdateFleetGroup :execrows
UPDATE fleet_groups
SET label       = $1,
    description = $2,
    updated_at  = $3
WHERE id = $4;

-- name: DeleteFleetGroup :execrows
DELETE FROM fleet_groups WHERE id = $1;
