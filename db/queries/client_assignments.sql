-- R-Q-03: client_assignments — many-to-many between a managed client
-- and its target (fleet group OR explicit agent).

-- name: ListClientAssignments :many
SELECT id, client_id, target_type, fleet_group_id, agent_id, created_at
FROM client_assignments
WHERE client_id = $1
ORDER BY created_at, id;

-- name: ListAllClientAssignments :many
SELECT id, client_id, target_type, fleet_group_id, agent_id, created_at
FROM client_assignments
ORDER BY client_id, created_at, id;

-- name: InsertClientAssignment :exec
INSERT INTO client_assignments (id, client_id, target_type, fleet_group_id, agent_id, created_at)
VALUES ($1, $2, $3, $4, $5, $6);

-- name: DeleteClientAssignmentsForClient :exec
DELETE FROM client_assignments WHERE client_id = $1;
