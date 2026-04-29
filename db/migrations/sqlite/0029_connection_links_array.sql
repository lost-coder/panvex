-- +goose Up
-- +goose NO TRANSACTION
-- Replace the single-link `connection_link` column on client_deployments
-- and discovered_clients with `connection_links` carrying a JSON array.
-- Telemt's tls_domains config emits one TLS link per domain (×host); the
-- agent used to collapse that to a single string and the panel never saw
-- the alternates the operator configured.
--
-- Dev-stage: drop the legacy column outright. Existing rows with a single
-- non-empty link are migrated into the new JSON array via a one-shot
-- UPDATE before the column drop so live data isn't lost.

PRAGMA foreign_keys = OFF;

-- ─── client_deployments ──────────────────────────────────────────────
ALTER TABLE client_deployments ADD COLUMN connection_links TEXT NOT NULL DEFAULT '[]';

UPDATE client_deployments
SET connection_links = json_array(connection_link)
WHERE connection_link != '';

ALTER TABLE client_deployments DROP COLUMN connection_link;

-- ─── discovered_clients ──────────────────────────────────────────────
ALTER TABLE discovered_clients ADD COLUMN connection_links TEXT NOT NULL DEFAULT '[]';

UPDATE discovered_clients
SET connection_links = json_array(connection_link)
WHERE connection_link != '';

ALTER TABLE discovered_clients DROP COLUMN connection_link;

PRAGMA foreign_keys = ON;

-- +goose Down
-- Dev-stage: drop+recreate acceptable, no rollback.
SELECT 1;
