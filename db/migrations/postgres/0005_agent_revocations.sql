-- P1-SEC-06: persist the set of deregistered agent IDs so that a control-plane
-- restart cannot "forget" a revocation. Without this table, an agent whose
-- admin deleted it but whose 30-day mTLS client cert is still valid could
-- reconnect and be accepted until cert expiry.
--
-- Cleanup removes rows once the cert has expired (cert_expires_at < now):
-- at that point the certificate can no longer authenticate regardless of
-- whether the agent_id is on the revocation list.
CREATE TABLE IF NOT EXISTS agent_revocations (
    agent_id         TEXT PRIMARY KEY,
    revoked_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    cert_expires_at  TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_agent_revocations_cert_expires_at
    ON agent_revocations(cert_expires_at);
