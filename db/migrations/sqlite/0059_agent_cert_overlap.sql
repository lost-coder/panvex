-- +goose Up
-- R11: certificate-overlap window for agent credential rotation.
-- Mirror of db/migrations/postgres/0059_agent_cert_overlap.sql — see the
-- rationale there. Plain ADD COLUMNs: SQLite handles these without a table
-- rebuild. The SPKI-length CHECK rides along on the ADD COLUMN, matching the
-- named constraint on the Postgres side and the inline CHECK the pre-existing
-- cert_spki_sha256 column already carries in 0001_init.
--
-- An earlier revision of this file omitted the CHECK, claiming SQLite cannot
-- add one via ALTER. That is false: SQLite accepts a column-constraint in
-- ALTER TABLE ADD COLUMN, stores it in the table DDL, and enforces it on
-- subsequent writes. The omission made the two dialects diverge and tripped
-- TestSchemaSyncPostgresMatchesSQLite.

ALTER TABLE agents ADD COLUMN cert_serial_prev TEXT NOT NULL DEFAULT '';
ALTER TABLE agents ADD COLUMN cert_spki_sha256_prev BLOB NOT NULL DEFAULT x''
    CHECK (length(cert_spki_sha256_prev) IN (0, 32));
ALTER TABLE agents ADD COLUMN cert_overlap_until_unix INTEGER;

-- +goose Down
ALTER TABLE agents DROP COLUMN cert_overlap_until_unix;
ALTER TABLE agents DROP COLUMN cert_spki_sha256_prev;
ALTER TABLE agents DROP COLUMN cert_serial_prev;
