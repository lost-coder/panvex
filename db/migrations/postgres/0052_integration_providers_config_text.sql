-- +goose Up
-- +goose NO TRANSACTION
-- bug1/H-6: integration_providers.config is vault-encrypted as a WHOLE
-- blob, not per-field. fleet.Service.encryptProviderConfig
-- (internal/controlplane/fleet/service.go) seals this column's plaintext
-- under the vault's "integration_config" domain whenever a secretvault is
-- configured (SetVault) — the production default, since
-- config.LoadBootstrap requires PANVEX_ENCRYPTION_KEY. The sealed value is
-- a "PVS1:"/"PVS2:"/"PVS3:"-prefixed ciphertext string, not JSON.
--
-- On PostgreSQL this column was JSONB (db/migrations/postgres/0001_init.sql),
-- and postgres/integrations.go's CreateIntegrationProvider/
-- UpdateIntegrationProvider bound the config parameter with an explicit
-- `$N::jsonb` cast. That cast rejects any non-JSON string at write time,
-- so creating or updating an integration provider on a PostgreSQL-backed
-- install with vault encryption enabled failed with "invalid input syntax
-- for type json" (SQLSTATE 22P02) on every write — the write path was
-- completely broken whenever the (default-on) vault was active.
--
-- SQLite never had this problem: integration_providers.config there is
-- plain TEXT, and db/migrations/sqlite/0052_json_valid_checks.sql gave it
-- a permissive CHECK — `json_valid(config) OR config LIKE 'PVS_:%'` — that
-- accepts either plain JSON or a vault-sealed ciphertext string. This
-- migration brings PostgreSQL to the same shape: the column becomes TEXT
-- (dropping JSONB's native validation, which is exactly the problem) and
-- gains an equivalent CHECK expressed with PostgreSQL's `IS JSON`
-- predicate (available since PG 16; this project's CI runs postgres:16-
-- alpine and prod runs postgres:18-alpine, so the predicate is safe to
-- rely on unconditionally).
--
-- fleet_group_integrations.config is intentionally NOT touched: only
-- provider configs ever go through encryptProviderConfig/
-- decryptProviderConfig (fleet/service.go's CreateProvider, UpdateProvider,
-- GetProvider, ListProviders call sites) — fleet-group-integration configs
-- are always plain, never-encrypted JSON, so that column's `::jsonb` casts
-- in postgres/integrations.go and its plain JSONB type remain correct.
--
-- NO TRANSACTION pragma: matches the convention used by other single-table
-- ALTER TABLE migrations in this bundle (e.g. 0051); ALTER COLUMN TYPE here
-- takes an ACCESS EXCLUSIVE lock and rewrites the table, but that rewrite
-- itself does not require running outside a transaction — NO TRANSACTION
-- is kept for consistency with the rest of this migration's straightforward
-- single-statement-group shape and to avoid goose wrapping unrelated DDL
-- together.
ALTER TABLE integration_providers
    ALTER COLUMN config TYPE text USING config::text;

-- ALTER COLUMN TYPE preserves the NOT NULL flag automatically, but the
-- DEFAULT survives as a stale `'{}'::jsonb` expression (verified against a
-- scratch PostgreSQL 18 table: `\d+` after the TYPE change still shows
-- `default '{}'::jsonb` even though the column is now text). PostgreSQL
-- happens to still accept inserts against that stale default because it
-- implicitly casts jsonb back to text at execution time, but the
-- rendered default is misleading (claims a type the column no longer has)
-- and would confuse both the schema-sync comparator's diagnostics output
-- and any human reading `\d+`. Re-declare it explicitly as a plain text
-- default so introspection reports the column truthfully.
ALTER TABLE integration_providers
    ALTER COLUMN config SET DEFAULT '{}';

-- Compensating control matching SQLite's permissive json_valid CHECK (see
-- header). The single-char wildcard in 'PVS_:%' covers all three sealed-
-- prefix generations (PVS1/PVS2/PVS3) without hardcoding each one, mirroring
-- the SQLite side's `LIKE 'PVS_:%'` pattern exactly.
ALTER TABLE integration_providers
    ADD CONSTRAINT integration_providers_config_check
    CHECK (config LIKE 'PVS_:%' OR config IS JSON);

-- +goose Down
-- Reversing this is best-effort: it will fail with a cast error if any row
-- currently holds a vault-sealed "PVSn:"-prefixed ciphertext string, since
-- that string is not valid JSON and cannot be cast back to jsonb. This
-- mirrors how the forward migration itself only became safe to write
-- because, at deploy time, no row is yet ciphertext — the Down direction
-- carries no such guarantee once the fix has been live and providers have
-- been created/updated under vault encryption. Operators rolling back after
-- go-live must first re-encrypt or clear sealed rows out-of-band; this is
-- an accepted, documented limitation (same posture as the irreversible-ish
-- Down blocks elsewhere in this bundle, e.g. 0044's dropped-column Down).
ALTER TABLE integration_providers
    DROP CONSTRAINT integration_providers_config_check;

ALTER TABLE integration_providers
    ALTER COLUMN config TYPE jsonb USING config::jsonb;

ALTER TABLE integration_providers
    ALTER COLUMN config SET DEFAULT '{}';
