-- +goose Up
-- S-01: configurable minimum password length on panel_settings.
-- SQLite ALTER TABLE supports ADD COLUMN with NOT NULL+DEFAULT since 3.35.
-- CHECK constraint must be inline (cannot ADD CHECK on existing column).
ALTER TABLE panel_settings ADD COLUMN password_min_length INTEGER NOT NULL DEFAULT 10;

-- +goose Down
-- SQLite < 3.35 cannot DROP COLUMN; modern builds (>= 3.35) used by
-- modernc.org/sqlite v1.50 support it.
ALTER TABLE panel_settings DROP COLUMN password_min_length;
