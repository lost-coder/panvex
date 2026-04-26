-- +goose Up
-- R-S-14: SQLite mirror of postgres migration 0025. fleet_groups.id is
-- TEXT (not UUID) on the SQLite side — schema 0014 keeps that legacy
-- shape. ON DELETE CASCADE matches the postgres semantics.
CREATE TABLE IF NOT EXISTS user_fleet_group_scopes (
    user_id        TEXT NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    fleet_group_id TEXT NOT NULL REFERENCES fleet_groups (id) ON DELETE CASCADE,
    granted_at_unix INTEGER NOT NULL DEFAULT 0,
    granted_by     TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (user_id, fleet_group_id)
);

CREATE INDEX IF NOT EXISTS idx_user_fleet_group_scopes_user_id
    ON user_fleet_group_scopes (user_id);
CREATE INDEX IF NOT EXISTS idx_user_fleet_group_scopes_fleet_group_id
    ON user_fleet_group_scopes (fleet_group_id);

-- +goose Down
DROP TABLE IF EXISTS user_fleet_group_scopes;
