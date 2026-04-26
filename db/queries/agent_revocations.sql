-- R-Q-03: agent_revocations — durable record of deregistered agents
-- whose mTLS client cert is still within validity. Persisted so a
-- restart cannot accidentally re-trust a revoked agent (P1-SEC-06).

-- name: UpsertAgentRevocation :exec
-- cert_expires_at is max-merged so a redundant revocation issued
-- against an older cert does not shrink the window.
INSERT INTO agent_revocations (agent_id, revoked_at, cert_expires_at)
VALUES ($1, $2, $3)
ON CONFLICT (agent_id) DO UPDATE SET
    revoked_at = EXCLUDED.revoked_at,
    cert_expires_at = GREATEST(agent_revocations.cert_expires_at, EXCLUDED.cert_expires_at);

-- name: ListAgentRevocations :many
SELECT agent_id, revoked_at, cert_expires_at FROM agent_revocations;

-- name: DeleteExpiredAgentRevocations :execrows
DELETE FROM agent_revocations WHERE cert_expires_at < $1;
