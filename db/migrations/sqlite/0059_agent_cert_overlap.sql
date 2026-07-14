-- +goose Up
-- R11: certificate-overlap window for agent credential rotation.
-- Mirror of db/migrations/postgres/0059_agent_cert_overlap.sql — see the
-- rationale there. Plain ADD COLUMNs: SQLite handles these without a table
-- rebuild, and the CHECK on the SPKI length is expressed on the Postgres side
-- only (SQLite cannot add a CHECK via ALTER, and the store writes the column
-- through one code path that always produces 0 or 32 bytes).

ALTER TABLE agents ADD COLUMN cert_serial_prev TEXT NOT NULL DEFAULT '';
ALTER TABLE agents ADD COLUMN cert_spki_sha256_prev BLOB NOT NULL DEFAULT x'';
ALTER TABLE agents ADD COLUMN cert_overlap_until_unix INTEGER;

-- +goose Down
ALTER TABLE agents DROP COLUMN cert_overlap_until_unix;
ALTER TABLE agents DROP COLUMN cert_spki_sha256_prev;
ALTER TABLE agents DROP COLUMN cert_serial_prev;
