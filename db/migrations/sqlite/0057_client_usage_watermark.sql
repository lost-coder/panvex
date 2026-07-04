-- +goose Up
-- P4 (cumulative traffic counters, audit 2026-07-02 #8): add the
-- per-(client, agent) watermark of the agent's cumulative counter:
--   agent_boot_id    — reporting agent process epoch (UUID; '' until the
--                      first cumulative report, e.g. discovery-seeded rows),
--   last_total_bytes — last cumulative total seen for that epoch.
-- The panel accumulates max(total - last_total, 0) into
-- traffic_used_bytes and rewrites the watermark; a new boot_id restarts
-- the epoch. Additive step: last_seq keeps working until the panel
-- cutover; it is dropped in 0058.
ALTER TABLE client_usage ADD COLUMN agent_boot_id TEXT NOT NULL DEFAULT '';
ALTER TABLE client_usage ADD COLUMN last_total_bytes INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE client_usage DROP COLUMN last_total_bytes;
ALTER TABLE client_usage DROP COLUMN agent_boot_id;
