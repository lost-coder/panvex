-- +goose Up
-- Add transport-mode fields to agents so the panel can distinguish inbound
-- agents (the default: agent dials the panel) from outbound/reverse-tunnel
-- agents (panel dials the agent via a stored dial_address).
-- bootstrap_* columns support a one-time token exchange for outbound agents.

ALTER TABLE agents ADD COLUMN transport_mode TEXT NOT NULL DEFAULT 'inbound'
    CHECK (transport_mode IN ('inbound', 'outbound'));
ALTER TABLE agents ADD COLUMN dial_address TEXT;
ALTER TABLE agents ADD COLUMN bootstrap_state TEXT NOT NULL DEFAULT 'active'
    CHECK (bootstrap_state IN ('pending', 'active', 'expired', 'revoked'));
ALTER TABLE agents ADD COLUMN bootstrap_token_hash BLOB;
ALTER TABLE agents ADD COLUMN bootstrap_expires_at INTEGER;

CREATE INDEX idx_agents_transport_mode ON agents(transport_mode);

-- Existing agents default to bootstrap_state=active (already enrolled).
-- New outbound agents are created with bootstrap_state=pending
-- (see bootstrap-flow in subsequent tasks).

-- +goose Down
DROP INDEX IF EXISTS idx_agents_transport_mode;
