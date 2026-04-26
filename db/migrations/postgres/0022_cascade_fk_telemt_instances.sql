-- +goose Up
-- Q4.U-D-03: drop the legacy unconstrained FK and re-add it with
-- ON DELETE CASCADE so deleting an agent also drops its instances.
-- SQLite already has CASCADE here via 0012_cascade_fk.sql; this brings
-- postgres into parity.
ALTER TABLE telemt_instances DROP CONSTRAINT IF EXISTS telemt_instances_agent_id_fkey;
ALTER TABLE telemt_instances
    ADD CONSTRAINT telemt_instances_agent_id_fkey
        FOREIGN KEY (agent_id) REFERENCES agents (id) ON DELETE CASCADE;

-- +goose Down
ALTER TABLE telemt_instances DROP CONSTRAINT IF EXISTS telemt_instances_agent_id_fkey;
ALTER TABLE telemt_instances
    ADD CONSTRAINT telemt_instances_agent_id_fkey
        FOREIGN KEY (agent_id) REFERENCES agents (id);
