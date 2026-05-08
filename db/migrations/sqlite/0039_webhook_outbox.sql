-- +goose Up
-- 0039: webhook outbox + endpoints (SQLite mirror of postgres).
-- SQLite: BOOLEAN as INTEGER (0/1), JSONB as TEXT (stored as JSON),
-- TIMESTAMPTZ as TIMESTAMP. The store layer normalises both
-- backends to the same Go types.

CREATE TABLE webhook_endpoints (
    id                 TEXT PRIMARY KEY,
    name               TEXT NOT NULL UNIQUE,
    url                TEXT NOT NULL,
    secret_ciphertext  TEXT NOT NULL,
    event_filter       TEXT NOT NULL DEFAULT '',
    allow_private      INTEGER NOT NULL DEFAULT 0 CHECK (allow_private IN (0, 1)),
    enabled            INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0, 1)),
    created_at         TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at         TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE webhook_outbox (
    id              TEXT PRIMARY KEY,
    endpoint_id     TEXT NOT NULL REFERENCES webhook_endpoints(id) ON DELETE CASCADE,
    event_action    TEXT NOT NULL,
    payload         TEXT NOT NULL,
    attempt         INTEGER NOT NULL DEFAULT 0,
    next_attempt_at TIMESTAMP NOT NULL,
    last_error      TEXT NOT NULL DEFAULT '',
    dead            INTEGER NOT NULL DEFAULT 0 CHECK (dead IN (0, 1)),
    created_at      TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    delivered_at    TIMESTAMP
);

CREATE INDEX idx_webhook_outbox_ready
    ON webhook_outbox (next_attempt_at)
    WHERE dead = 0 AND delivered_at IS NULL;

-- +goose Down
-- intentionally empty (pre-release, no compatibility shim)
