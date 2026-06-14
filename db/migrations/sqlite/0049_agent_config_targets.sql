-- +goose Up
-- agent_config_targets stores the operator's DESIRED Telemt config per scope.
-- scope_type is 'group' (scope_id = fleet_groups.id) or 'agent' (scope_id =
-- agents.id). sections_json is a sparse JSON object of editable config sections
-- (general/timeouts/censorship/upstreams/show_link/dc_overrides). The effective
-- config of an agent is its group target merged with its agent override.
-- SQLite: TIMESTAMPTZ as TIMESTAMP (store layer normalises to Go types).
CREATE TABLE IF NOT EXISTS agent_config_targets (
    scope_type    TEXT NOT NULL,
    scope_id      TEXT NOT NULL,
    sections_json TEXT NOT NULL DEFAULT '{}',
    created_at    TIMESTAMP NOT NULL,
    updated_at    TIMESTAMP NOT NULL,
    PRIMARY KEY (scope_type, scope_id)
);

-- +goose Down
DROP TABLE IF EXISTS agent_config_targets;
