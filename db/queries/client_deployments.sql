-- R-Q-03: client_deployments — per-(client, agent) deployment state
-- + connection link returned by the agent.

-- name: ListClientDeployments :many
SELECT client_id, agent_id, desired_operation, status, last_error,
       connection_links, link_diagnostic, last_applied_at, updated_at,
       last_reset_epoch_secs
FROM client_deployments
WHERE client_id = $1
ORDER BY agent_id ASC;


-- name: ListAllClientDeployments :many
SELECT client_id, agent_id, desired_operation, status, last_error,
       connection_links, link_diagnostic, last_applied_at, updated_at,
       last_reset_epoch_secs
FROM client_deployments
ORDER BY client_id ASC, agent_id ASC;


-- name: UpsertClientDeployment :exec
INSERT INTO client_deployments (client_id, agent_id, desired_operation,
                                status, last_error, connection_links,
                                link_diagnostic,
                                last_applied_at, updated_at,
                                last_reset_epoch_secs)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
ON CONFLICT (client_id, agent_id) DO UPDATE
SET desired_operation = EXCLUDED.desired_operation,
    status            = EXCLUDED.status,
    last_error        = EXCLUDED.last_error,
    connection_links   = EXCLUDED.connection_links,
    link_diagnostic   = EXCLUDED.link_diagnostic,
    last_applied_at   = EXCLUDED.last_applied_at,
    updated_at        = EXCLUDED.updated_at,
    last_reset_epoch_secs = EXCLUDED.last_reset_epoch_secs;

-- name: DeleteClientDeploymentsForClient :exec
DELETE FROM client_deployments WHERE client_id = $1;

-- name: UpdateClientDeploymentLastReset :exec
-- Phase 3 (reset-quota): bump last_reset_epoch_secs after a successful
-- client.reset_quota job lands. Kept separate from UpsertClientDeployment
-- so the job-completion path doesn't have to re-supply every column of
-- the row.
UPDATE client_deployments
SET last_reset_epoch_secs = $3,
    updated_at = $4
WHERE client_id = $1 AND agent_id = $2;
