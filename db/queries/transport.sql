-- name: GetAgentTransport :one
SELECT id, transport_mode, dial_address, bootstrap_state,
       bootstrap_token_hash, bootstrap_expires_at
FROM agents
WHERE id = $1;


-- name: ListAgentsByTransportMode :many
SELECT id, transport_mode, dial_address, bootstrap_state
FROM agents
WHERE transport_mode = $1;


-- name: UpdateAgentTransportMode :exec
UPDATE agents
SET transport_mode = $2,
    dial_address = $3
WHERE id = $1;


-- name: SetAgentBootstrapToken :exec
UPDATE agents
SET bootstrap_state = 'pending',
    bootstrap_token_hash = $2,
    bootstrap_expires_at = $3
WHERE id = $1;


-- name: ClearAgentBootstrapToken :exec
UPDATE agents
SET bootstrap_state = 'active',
    bootstrap_token_hash = NULL,
    bootstrap_expires_at = NULL
WHERE id = $1;


-- name: ExpireAgentBootstrapToken :exec
UPDATE agents
SET bootstrap_state = 'expired',
    bootstrap_token_hash = NULL
WHERE id = $1;
