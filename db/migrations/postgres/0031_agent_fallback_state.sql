-- +goose Up
CREATE TABLE IF NOT EXISTS agent_fallback_state (
    agent_id        TEXT PRIMARY KEY REFERENCES agents (id) ON DELETE CASCADE,
    entered_at_unix BIGINT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_agent_fallback_state_entered_at
    ON agent_fallback_state (entered_at_unix);

-- +goose Down
DROP INDEX IF EXISTS idx_agent_fallback_state_entered_at;
DROP TABLE IF EXISTS agent_fallback_state;
