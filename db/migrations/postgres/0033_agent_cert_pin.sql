-- +goose Up
-- +goose NO TRANSACTION
-- S-02: pin the SubjectPublicKeyInfo (SPKI) SHA-256 of the agent's serving
-- certificate. Set on first successful enroll; subsequent dials verify the
-- presented cert hashes to the same value. Defends against MITM during
-- post-enroll handshakes when an attacker holds a forged cert from any CA
-- the agent trusts.
--
-- NO TRANSACTION pragma: required because CREATE/DROP INDEX CONCURRENTLY
-- cannot run inside a transaction. ALTER TABLE ADD COLUMN does take an
-- ACCESS EXCLUSIVE lock briefly, but the column has a constant DEFAULT
-- so PG 11+ skips the table rewrite.
ALTER TABLE agents
    ADD COLUMN cert_spki_sha256 BYTEA NOT NULL DEFAULT ''::bytea
    CHECK (length(cert_spki_sha256) IN (0, 32));

CREATE INDEX CONCURRENTLY idx_agents_cert_spki_sha256
    ON agents (cert_spki_sha256)
    WHERE length(cert_spki_sha256) > 0;

-- +goose Down
DROP INDEX CONCURRENTLY IF EXISTS idx_agents_cert_spki_sha256;
ALTER TABLE agents DROP COLUMN IF EXISTS cert_spki_sha256;
