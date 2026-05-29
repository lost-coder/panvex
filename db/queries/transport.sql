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


-- name: ConsumeAgentBootstrapToken :execrows
-- Atomically claim a pending bootstrap token: flips the state out of
-- 'pending' so only the first concurrent enrollment proceeds. The
-- token hash/expiry are intentionally preserved so a sign failure can
-- revert the claim (RevertAgentBootstrapConsumed) and let the operator
-- retry. Returns the number of rows affected (1 = claimed, 0 = already
-- consumed by a concurrent enrollment / replay within the TTL).
UPDATE agents
SET bootstrap_state = 'active'
WHERE id = $1 AND bootstrap_state = 'pending';


-- name: RevertAgentBootstrapConsumed :exec
-- Roll a claimed-but-not-completed token back to 'pending' for retry.
-- Guarded by bootstrap_token_hash IS NOT NULL so it only ever reverts a
-- row still mid-enrollment (the token has not been cleared), never a
-- fully-enrolled agent.
UPDATE agents
SET bootstrap_state = 'pending'
WHERE id = $1 AND bootstrap_state = 'active' AND bootstrap_token_hash IS NOT NULL;
