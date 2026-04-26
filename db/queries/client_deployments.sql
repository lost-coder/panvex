-- R-Q-03: client_deployments — per-(client, agent) deployment state
-- + connection link returned by the agent.

-- name: ListClientDeployments :many
SELECT client_id, agent_id, desired_operation, status, last_error,
       connection_link, last_applied_at, updated_at
FROM client_deployments
WHERE client_id = $1
ORDER BY agent_id;

-- name: ListAllClientDeployments :many
SELECT client_id, agent_id, desired_operation, status, last_error,
       connection_link, last_applied_at, updated_at
FROM client_deployments
ORDER BY client_id, agent_id;

-- name: UpsertClientDeployment :exec
INSERT INTO client_deployments (client_id, agent_id, desired_operation,
                                status, last_error, connection_link,
                                last_applied_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT (client_id, agent_id) DO UPDATE
SET desired_operation = EXCLUDED.desired_operation,
    status            = EXCLUDED.status,
    last_error        = EXCLUDED.last_error,
    connection_link   = EXCLUDED.connection_link,
    last_applied_at   = EXCLUDED.last_applied_at,
    updated_at        = EXCLUDED.updated_at;

-- name: DeleteClientDeploymentsForClient :exec
DELETE FROM client_deployments WHERE client_id = $1;
