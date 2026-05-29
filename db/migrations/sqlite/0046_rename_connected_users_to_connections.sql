-- +goose Up
-- IN-M3: rename telemt_instances.connected_users -> connections (see the
-- Postgres 0046 mirror). The column holds the telemt CurrentConnections
-- counter, not a users count. SQLite 3.25+ supports RENAME COLUMN inline.
ALTER TABLE telemt_instances RENAME COLUMN connected_users TO connections;

-- +goose Down
ALTER TABLE telemt_instances RENAME COLUMN connections TO connected_users;
