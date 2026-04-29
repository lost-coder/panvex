-- +goose Up
-- Replace the single-link connection_link column with a JSON array
-- (connection_links) on both client_deployments and discovered_clients.
-- Telemt's tls_domains config emits one TLS link per domain (×host); the
-- agent used to collapse that to a single string. The panel now stores
-- and surfaces the full array.
--
-- Dev-stage: drop the legacy column. Existing rows with a single non-
-- empty link are migrated into the new JSON array first.

ALTER TABLE client_deployments
    ADD COLUMN connection_links JSONB NOT NULL DEFAULT '[]'::jsonb;

UPDATE client_deployments
SET connection_links = jsonb_build_array(connection_link)
WHERE connection_link <> '';

ALTER TABLE client_deployments DROP COLUMN connection_link;

ALTER TABLE discovered_clients
    ADD COLUMN connection_links JSONB NOT NULL DEFAULT '[]'::jsonb;

UPDATE discovered_clients
SET connection_links = jsonb_build_array(connection_link)
WHERE connection_link <> '';

ALTER TABLE discovered_clients DROP COLUMN connection_link;

-- +goose Down
-- Dev-stage: drop+recreate acceptable, no rollback.
SELECT 1;
