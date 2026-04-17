-- +goose Up
-- NOTE: SQLite's ALTER TABLE ADD COLUMN has no "IF NOT EXISTS" clause.
-- Goose's version tracking ensures this migration only runs once per database,
-- so an unconditional ADD COLUMN is safe here — the previous hand-rolled
-- ensureAgentsCertIssuedAtColumn helpers became necessary only because the
-- ad-hoc migrator had no way to know whether the column was already applied.
ALTER TABLE agents ADD COLUMN cert_issued_at_unix INTEGER;
ALTER TABLE agents ADD COLUMN cert_expires_at_unix INTEGER;

-- +goose Down
-- SQLite supports DROP COLUMN since 3.35 (modernc.org/sqlite ships 3.47+),
-- but the operation rewrites the table; for a pre-3.35 runtime this Down
-- would need a table-rename approach. Best-effort on modern SQLite.
ALTER TABLE agents DROP COLUMN cert_expires_at_unix;
ALTER TABLE agents DROP COLUMN cert_issued_at_unix;
