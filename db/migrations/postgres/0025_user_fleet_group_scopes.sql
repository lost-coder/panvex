-- +goose Up
-- R-S-14: per-user fleet-group scope mapping. An operator with at
-- least one row in this table is restricted to those fleet groups —
-- their /clients, /fleet-groups, /discovered-clients, and job-target
-- queries are filtered to the union of their scopes. An operator with
-- no rows here keeps the legacy global view (single-tenant default).
-- Admins are always global regardless of rows.
--
-- Cascade DELETE on both sides so removing a user or a fleet group
-- automatically tidies the join rows; no orphans, no manual cleanup.
CREATE TABLE IF NOT EXISTS user_fleet_group_scopes (
    user_id        TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    fleet_group_id UUID NOT NULL REFERENCES fleet_groups (id) ON DELETE CASCADE,
    granted_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    granted_by     TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (user_id, fleet_group_id)
);

CREATE INDEX IF NOT EXISTS idx_user_fleet_group_scopes_user_id
    ON user_fleet_group_scopes (user_id);
CREATE INDEX IF NOT EXISTS idx_user_fleet_group_scopes_fleet_group_id
    ON user_fleet_group_scopes (fleet_group_id);

-- +goose Down
DROP TABLE IF EXISTS user_fleet_group_scopes;
