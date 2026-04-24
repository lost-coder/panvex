-- +goose Up
-- Fleet groups redesign (breaking): `id` becomes UUID. The old schema
-- stored it as a semantic slug ("default", "edge") that cannot be
-- auto-cast; dependent FK rows are cleared so pre-release operators
-- re-enroll agents after upgrade. See sqlite/0014 for the equivalent
-- path on SQLite (where `id` remains TEXT and UUID format is
-- enforced at the application layer).
--
-- The display surface splits into `name` (immutable slug — unique) +
-- `label` (editable) + `description`. Adds an extensibility layer so
-- integrations (DNS round-robin, webhooks, future plugins) attach
-- per-group without schema churn every time a new kind ships.
DELETE FROM client_assignments;
DELETE FROM client_deployments;
DELETE FROM discovered_clients;
DELETE FROM enrollment_tokens;
DELETE FROM telemt_runtime_events;
DELETE FROM telemt_runtime_upstreams_current;
DELETE FROM telemt_runtime_dcs_current;
DELETE FROM telemt_runtime_current;
DELETE FROM telemt_diagnostics_current;
DELETE FROM telemt_security_inventory_current;
DELETE FROM telemt_instances;
DELETE FROM agents;
DELETE FROM fleet_groups;

-- Drop FK constraints first: Postgres refuses to retype the parent PK
-- while child columns still reference it with a mismatched type, even
-- if the dependent tables were just truncated above.
ALTER TABLE agents
    DROP CONSTRAINT IF EXISTS agents_fleet_group_id_fkey;
ALTER TABLE enrollment_tokens
    DROP CONSTRAINT IF EXISTS enrollment_tokens_fleet_group_id_fkey;
ALTER TABLE client_assignments
    DROP CONSTRAINT IF EXISTS client_assignments_fleet_group_id_fkey,
    DROP CONSTRAINT IF EXISTS fk_client_assignments_fleet_group_id;

ALTER TABLE fleet_groups
    ALTER COLUMN id DROP DEFAULT,
    ALTER COLUMN id TYPE UUID USING gen_random_uuid(),
    ADD COLUMN label TEXT NOT NULL DEFAULT '',
    ADD COLUMN description TEXT NOT NULL DEFAULT '',
    ADD COLUMN updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW();

ALTER TABLE fleet_groups
    ADD CONSTRAINT fleet_groups_name_unique UNIQUE (name);

-- Dependent FK columns → UUID, then reattach FKs.
ALTER TABLE agents
    ALTER COLUMN fleet_group_id TYPE UUID USING NULL,
    ADD CONSTRAINT agents_fleet_group_id_fkey
        FOREIGN KEY (fleet_group_id) REFERENCES fleet_groups (id);

ALTER TABLE enrollment_tokens
    ALTER COLUMN fleet_group_id TYPE UUID USING NULL,
    ADD CONSTRAINT enrollment_tokens_fleet_group_id_fkey
        FOREIGN KEY (fleet_group_id) REFERENCES fleet_groups (id);

ALTER TABLE client_assignments
    ALTER COLUMN fleet_group_id TYPE UUID USING NULL,
    ADD CONSTRAINT fk_client_assignments_fleet_group_id
        FOREIGN KEY (fleet_group_id) REFERENCES fleet_groups (id) ON DELETE SET NULL;

-- Shared credential store so a single provider config (e.g. one
-- Cloudflare account) can back DNS integrations for many groups.
CREATE TABLE IF NOT EXISTS integration_providers (
    id UUID PRIMARY KEY,
    kind TEXT NOT NULL,
    label TEXT NOT NULL DEFAULT '',
    config JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_integration_providers_kind
    ON integration_providers (kind);

-- Per-group integration install: at most one row per (group, kind).
-- provider_id is nullable because not every integration ships with a
-- shared credential — a local-only integration may embed its whole
-- config inline.
CREATE TABLE IF NOT EXISTS fleet_group_integrations (
    id UUID PRIMARY KEY,
    fleet_group_id UUID NOT NULL REFERENCES fleet_groups (id) ON DELETE CASCADE,
    kind TEXT NOT NULL,
    provider_id UUID REFERENCES integration_providers (id) ON DELETE SET NULL,
    config JSONB NOT NULL DEFAULT '{}'::jsonb,
    enabled BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
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

ALTER TABLE client_assignments
    ALTER COLUMN fleet_group_id TYPE TEXT USING NULL;
ALTER TABLE enrollment_tokens
    ALTER COLUMN fleet_group_id TYPE TEXT USING NULL;
ALTER TABLE agents
    ALTER COLUMN fleet_group_id TYPE TEXT USING NULL;

ALTER TABLE fleet_groups
    DROP CONSTRAINT IF EXISTS fleet_groups_name_unique,
    DROP COLUMN IF EXISTS updated_at,
    DROP COLUMN IF EXISTS description,
    DROP COLUMN IF EXISTS label,
    ALTER COLUMN id TYPE TEXT USING id::TEXT;
