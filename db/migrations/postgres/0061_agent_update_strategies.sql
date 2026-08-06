-- +goose Up
-- Telemt Update v1 (Task 8): per-agent update strategy, persisted so the
-- panel and the agent agree on how `telemt.update` jobs should be applied
-- (binary swap vs docker vs disabled) without re-deriving it from runtime
-- probes on every request. One row per agent; deleting the agent removes
-- its strategy (ON DELETE CASCADE), mirroring agent_fallback_state.
-- Mirror of db/migrations/sqlite/0061_agent_update_strategies.sql.

CREATE TABLE public.agent_update_strategies (
    agent_id     text NOT NULL,
    mode         text NOT NULL,
    restart_spec text DEFAULT ''::text NOT NULL,
    binary_path  text DEFAULT ''::text NOT NULL,
    asset_flavor text DEFAULT ''::text NOT NULL,
    created_at   timestamp with time zone NOT NULL,
    updated_at   timestamp with time zone NOT NULL,
    PRIMARY KEY (agent_id),
    FOREIGN KEY (agent_id) REFERENCES public.agents (id) ON DELETE CASCADE
);

-- +goose Down
DROP TABLE public.agent_update_strategies;
