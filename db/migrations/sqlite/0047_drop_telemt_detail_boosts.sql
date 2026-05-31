-- +goose Up
-- F4: drop the persisted detail-boost table. Detail boost is an ephemeral
-- ~10m telemetry-frequency bump for a single agent and now lives only in
-- memory on the panel (s.detailBoosts). It is intentionally not durable —
-- if the panel restarts the operator simply re-enables it.
DROP TABLE IF EXISTS telemt_detail_boosts;

-- +goose Down
CREATE TABLE IF NOT EXISTS telemt_detail_boosts (
    agent_id TEXT PRIMARY KEY,
    expires_at_unix INTEGER NOT NULL,
    updated_at_unix INTEGER NOT NULL,
    FOREIGN KEY (agent_id) REFERENCES agents (id) ON DELETE CASCADE
);
