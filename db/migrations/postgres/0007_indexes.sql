-- +goose Up
CREATE INDEX IF NOT EXISTS idx_agents_last_seen_at ON agents (last_seen_at);
CREATE INDEX IF NOT EXISTS idx_agents_fleet_group_id ON agents (fleet_group_id);
CREATE INDEX IF NOT EXISTS idx_telemt_instances_agent_id ON telemt_instances (agent_id);
CREATE INDEX IF NOT EXISTS idx_client_assignments_client_id ON client_assignments (client_id);
CREATE INDEX IF NOT EXISTS idx_client_deployments_client_id ON client_deployments (client_id);
CREATE INDEX IF NOT EXISTS idx_jobs_created_at ON jobs (created_at);
CREATE INDEX IF NOT EXISTS idx_audit_events_created_at ON audit_events (created_at);
CREATE INDEX IF NOT EXISTS idx_metric_snapshots_agent_captured ON metric_snapshots (agent_id, captured_at);
CREATE INDEX IF NOT EXISTS idx_discovered_clients_agent_id ON discovered_clients (agent_id);
CREATE INDEX IF NOT EXISTS idx_ts_server_load_time ON ts_server_load (agent_id, captured_at DESC);
CREATE INDEX IF NOT EXISTS idx_ts_dc_health_time ON ts_dc_health (agent_id, captured_at DESC);
CREATE INDEX IF NOT EXISTS idx_client_ip_last_seen ON client_ip_history (last_seen);
CREATE INDEX IF NOT EXISTS idx_client_ip_client ON client_ip_history (client_id, last_seen DESC);
CREATE INDEX IF NOT EXISTS idx_client_ip_client_addr ON client_ip_history (client_id, ip_address);

-- +goose Down
DROP INDEX IF EXISTS idx_client_ip_client_addr;
DROP INDEX IF EXISTS idx_client_ip_client;
DROP INDEX IF EXISTS idx_client_ip_last_seen;
DROP INDEX IF EXISTS idx_ts_dc_health_time;
DROP INDEX IF EXISTS idx_ts_server_load_time;
DROP INDEX IF EXISTS idx_discovered_clients_agent_id;
DROP INDEX IF EXISTS idx_metric_snapshots_agent_captured;
DROP INDEX IF EXISTS idx_audit_events_created_at;
DROP INDEX IF EXISTS idx_jobs_created_at;
DROP INDEX IF EXISTS idx_client_deployments_client_id;
DROP INDEX IF EXISTS idx_client_assignments_client_id;
DROP INDEX IF EXISTS idx_telemt_instances_agent_id;
DROP INDEX IF EXISTS idx_agents_fleet_group_id;
DROP INDEX IF EXISTS idx_agents_last_seen_at;
