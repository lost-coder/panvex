-- +goose Up
-- Fleet groups redesign: see postgres/0014 for the full rationale.
-- SQLite supports ADD COLUMN but not CHANGE COLUMN, so we add the
-- new fields in place and backfill.
ALTER TABLE fleet_groups ADD COLUMN label           TEXT NOT NULL DEFAULT '';
ALTER TABLE fleet_groups ADD COLUMN description     TEXT NOT NULL DEFAULT '';
ALTER TABLE fleet_groups ADD COLUMN updated_at_unix INTEGER NOT NULL DEFAULT 0;

-- Backfill label from the existing name and seed updated_at from
-- created_at so pre-migration rows have plausible values.
UPDATE fleet_groups SET label = name WHERE label = '';
UPDATE fleet_groups SET updated_at_unix = created_at_unix WHERE updated_at_unix = 0;

-- SQLite pre-3.35 cannot ADD CONSTRAINT, but a UNIQUE index does the
-- same job and is lighter than rebuilding the table.
CREATE UNIQUE INDEX IF NOT EXISTS fleet_groups_name_unique
    ON fleet_groups (name);

CREATE TABLE IF NOT EXISTS integration_providers (
    id              TEXT PRIMARY KEY,
    kind            TEXT NOT NULL,
    label           TEXT NOT NULL DEFAULT '',
    config          TEXT NOT NULL DEFAULT '{}',
    created_at_unix INTEGER NOT NULL,
    updated_at_unix INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_integration_providers_kind
    ON integration_providers (kind);

CREATE TABLE IF NOT EXISTS fleet_group_integrations (
    id              TEXT PRIMARY KEY,
    fleet_group_id  TEXT NOT NULL,
    kind            TEXT NOT NULL,
    provider_id     TEXT,
    config          TEXT NOT NULL DEFAULT '{}',
    enabled         INTEGER NOT NULL DEFAULT 0,
    created_at_unix INTEGER NOT NULL,
    updated_at_unix INTEGER NOT NULL,
    FOREIGN KEY (fleet_group_id) REFERENCES fleet_groups (id) ON DELETE CASCADE,
    FOREIGN KEY (provider_id)    REFERENCES integration_providers (id) ON DELETE SET NULL,
    UNIQUE (fleet_group_id, kind)
);

CREATE INDEX IF NOT EXISTS idx_fleet_group_integrations_fleet_group_id
    ON fleet_group_integrations (fleet_group_id);
CREATE INDEX IF NOT EXISTS idx_fleet_group_integrations_kind
    ON fleet_group_integrations (kind);

-- +goose Down
DROP INDEX IF EXISTS idx_fleet_group_integrations_kind;
DROP INDEX IF EXISTS idx_fleet_group_integrations_fleet_group_id;
DROP TABLE IF EXISTS fleet_group_integrations;
DROP INDEX IF EXISTS idx_integration_providers_kind;
DROP TABLE IF EXISTS integration_providers;

-- SQLite supports DROP COLUMN from 3.35 (modernc.org/sqlite pins a
-- recent enough version). Index drop is idempotent.
DROP INDEX IF EXISTS fleet_groups_name_unique;
ALTER TABLE fleet_groups DROP COLUMN updated_at_unix;
ALTER TABLE fleet_groups DROP COLUMN description;
ALTER TABLE fleet_groups DROP COLUMN label;
