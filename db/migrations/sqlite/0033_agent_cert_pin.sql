-- +goose Up
-- S-02: SPKI pin column. SQLite stores BLOB; partial indexes are supported
-- since 3.8.0 (modernc/sqlite v1.50 ships SQLite 3.46+).
-- CHECK constraint must be inline (cannot ADD CHECK on existing column);
-- mirrors the same restriction documented in 0032_password_policy.sql.
ALTER TABLE agents ADD COLUMN cert_spki_sha256 BLOB NOT NULL DEFAULT x'';

CREATE INDEX idx_agents_cert_spki_sha256
    ON agents (cert_spki_sha256)
    WHERE length(cert_spki_sha256) > 0;

-- +goose Down
-- SQLite >= 3.35 supports DROP COLUMN (modernc v1.50 ships 3.46+).
DROP INDEX IF EXISTS idx_agents_cert_spki_sha256;
ALTER TABLE agents DROP COLUMN IF EXISTS cert_spki_sha256;
