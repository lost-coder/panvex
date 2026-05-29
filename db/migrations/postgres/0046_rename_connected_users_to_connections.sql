-- +goose Up
-- IN-M3: the telemt_instances.connected_users column actually holds the
-- telemt CurrentConnections counter (number of TCP connections), not a
-- distinct-users count. Rename the column end-to-end to reflect its true
-- meaning. Value semantics are unchanged.
ALTER TABLE telemt_instances RENAME COLUMN connected_users TO connections;

-- +goose Down
ALTER TABLE telemt_instances RENAME COLUMN connections TO connected_users;
