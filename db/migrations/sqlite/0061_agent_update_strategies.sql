-- +goose Up
-- Telemt Update v1 (Task 8): per-agent update strategy, persisted so the
-- panel and the agent agree on how `telemt.update` jobs should be applied
-- (binary swap vs docker vs disabled) without re-deriving it from runtime
-- probes on every request. One row per agent; deleting the agent removes
-- its strategy (ON DELETE CASCADE), mirroring agent_fallback_state.
-- Mirror of db/migrations/postgres/0061_agent_update_strategies.sql.

CREATE TABLE agent_update_strategies (
    agent_id     TEXT PRIMARY KEY,
    mode         TEXT NOT NULL,
    restart_spec TEXT NOT NULL DEFAULT '',
    binary_path  TEXT NOT NULL DEFAULT '',
    asset_flavor TEXT NOT NULL DEFAULT '',
    created_at   TIMESTAMP NOT NULL,
    updated_at   TIMESTAMP NOT NULL,
    FOREIGN KEY (agent_id) REFERENCES agents (id) ON DELETE CASCADE
);

-- +goose Down
DROP TABLE agent_update_strategies;
