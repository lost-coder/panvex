-- +goose Up
-- Add ON DELETE CASCADE to the remaining FKs on agents (id):
--   * agent_certificate_recovery_grants — never patched.
--   * discovered_clients                — never patched.
-- telemt_instances was handled in 0022_cascade_fk_telemt_instances.sql.
-- Without these, DELETE FROM agents fails when the deregistered agent
-- still has rows in either table (see http_agents.persistAgentDeregister).

ALTER TABLE agent_certificate_recovery_grants
    DROP CONSTRAINT IF EXISTS agent_certificate_recovery_grants_agent_id_fkey;
ALTER TABLE agent_certificate_recovery_grants
    ADD CONSTRAINT agent_certificate_recovery_grants_agent_id_fkey
        FOREIGN KEY (agent_id) REFERENCES agents (id) ON DELETE CASCADE;

ALTER TABLE discovered_clients
    DROP CONSTRAINT IF EXISTS discovered_clients_agent_id_fkey;
ALTER TABLE discovered_clients
    ADD CONSTRAINT discovered_clients_agent_id_fkey
        FOREIGN KEY (agent_id) REFERENCES agents (id) ON DELETE CASCADE;

-- +goose Down
ALTER TABLE agent_certificate_recovery_grants
    DROP CONSTRAINT IF EXISTS agent_certificate_recovery_grants_agent_id_fkey;
ALTER TABLE agent_certificate_recovery_grants
    ADD CONSTRAINT agent_certificate_recovery_grants_agent_id_fkey
        FOREIGN KEY (agent_id) REFERENCES agents (id);

ALTER TABLE discovered_clients
    DROP CONSTRAINT IF EXISTS discovered_clients_agent_id_fkey;
ALTER TABLE discovered_clients
    ADD CONSTRAINT discovered_clients_agent_id_fkey
        FOREIGN KEY (agent_id) REFERENCES agents (id);
