-- +goose Up
ALTER TABLE agents ADD COLUMN cert_serial TEXT NOT NULL DEFAULT '';

-- +goose Down
-- SQLite cannot DROP COLUMN inline on older versions; rebuild table.
CREATE TABLE agents_old AS SELECT id, node_name, fleet_group_id, version, read_only,
    last_seen_at_unix, cert_issued_at_unix, cert_expires_at_unix
    FROM agents;
DROP TABLE agents;
ALTER TABLE agents_old RENAME TO agents;
