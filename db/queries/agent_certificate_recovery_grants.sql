-- R-Q-03: agent_certificate_recovery_grants — admin-issued grants
-- that allow a replaced agent to bootstrap with a fresh cert.

-- name: GetAgentCertificateRecoveryGrant :one
SELECT agent_id, issued_by, issued_at, expires_at, used_at, revoked_at
FROM agent_certificate_recovery_grants
WHERE agent_id = $1;

-- name: ListAgentCertificateRecoveryGrants :many
SELECT agent_id, issued_by, issued_at, expires_at, used_at, revoked_at
FROM agent_certificate_recovery_grants
ORDER BY issued_at ASC, agent_id ASC;


-- name: UpsertAgentCertificateRecoveryGrant :exec
INSERT INTO agent_certificate_recovery_grants (agent_id, issued_by, issued_at, expires_at, used_at, revoked_at)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (agent_id) DO UPDATE
SET issued_by = EXCLUDED.issued_by,
    issued_at = EXCLUDED.issued_at,
    expires_at = EXCLUDED.expires_at,
    used_at = EXCLUDED.used_at,
    revoked_at = EXCLUDED.revoked_at;

-- name: MarkAgentCertificateRecoveryGrantUsed :execrows
UPDATE agent_certificate_recovery_grants
SET used_at = $1
WHERE agent_id = $2 AND used_at IS NULL AND revoked_at IS NULL;

-- name: RevokeAgentCertificateRecoveryGrant :execrows
UPDATE agent_certificate_recovery_grants
SET revoked_at = $1
WHERE agent_id = $2 AND used_at IS NULL AND revoked_at IS NULL;
