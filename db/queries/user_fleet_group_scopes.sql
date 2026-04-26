-- R-Q-03: user_fleet_group_scopes — per-user fleet-group scope
-- mapping introduced by R-S-14. The list/insert/delete queries
-- reflect the same semantics that the storage layer exposes.

-- name: ListUserFleetGroupScopes :many
SELECT fleet_group_id::text AS fleet_group_id
FROM user_fleet_group_scopes
WHERE user_id = $1
ORDER BY fleet_group_id;

-- name: ListAllUserFleetGroupScopes :many
SELECT user_id, fleet_group_id::text AS fleet_group_id, granted_at, granted_by
FROM user_fleet_group_scopes
ORDER BY user_id, fleet_group_id;

-- name: ClearUserFleetGroupScopes :exec
DELETE FROM user_fleet_group_scopes WHERE user_id = $1;

-- name: InsertUserFleetGroupScope :exec
INSERT INTO user_fleet_group_scopes (user_id, fleet_group_id, granted_at, granted_by)
VALUES ($1, $2, $3, $4);
