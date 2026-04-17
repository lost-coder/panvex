-- See postgres/0005_agent_revocations.sql for rationale. SQLite uses INTEGER
-- unix timestamps to match the existing convention.
CREATE TABLE IF NOT EXISTS agent_revocations (
    agent_id              TEXT PRIMARY KEY,
    revoked_at_unix       INTEGER NOT NULL,
    cert_expires_at_unix  INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_agent_revocations_cert_expires_at_unix
    ON agent_revocations(cert_expires_at_unix);
