-- +goose Up
-- Add transport-mode fields to agents so the panel can distinguish inbound
-- agents (the default: agent dials the panel) from outbound/reverse-tunnel
-- agents (panel dials the agent via a stored dial_address).
-- bootstrap_* columns support a one-time token exchange for outbound agents.

ALTER TABLE agents
    ADD COLUMN transport_mode text NOT NULL DEFAULT 'inbound'
        CHECK (transport_mode IN ('inbound', 'outbound')),
    ADD COLUMN dial_address text,
    ADD COLUMN bootstrap_state text NOT NULL DEFAULT 'active'
        CHECK (bootstrap_state IN ('pending', 'active', 'expired', 'revoked')),
    ADD COLUMN bootstrap_token_hash bytea,
    ADD COLUMN bootstrap_expires_at timestamptz;

-- +goose StatementBegin
-- +goose NO TRANSACTION
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_agents_transport_mode ON agents(transport_mode);
-- +goose StatementEnd

-- Existing agents default to bootstrap_state=active (already enrolled).
-- New outbound agents are created with bootstrap_state=pending
-- (see bootstrap-flow in subsequent tasks).

-- +goose Down
-- +goose NO TRANSACTION
DROP INDEX CONCURRENTLY IF EXISTS idx_agents_transport_mode;
ALTER TABLE agents
    DROP COLUMN IF EXISTS bootstrap_expires_at,
    DROP COLUMN IF EXISTS bootstrap_token_hash,
    DROP COLUMN IF EXISTS bootstrap_state,
    DROP COLUMN IF EXISTS dial_address,
    DROP COLUMN IF EXISTS transport_mode;
