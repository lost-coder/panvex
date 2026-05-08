-- +goose Up
-- 0039: webhook outbox + endpoints.
-- Two tables: endpoints define receivers; outbox queues pending
-- deliveries with retry / dead-letter state. Worker polls outbox,
-- HMAC-signs body, exponential-backoff on failure.

CREATE TABLE webhook_endpoints (
    id                 TEXT PRIMARY KEY,
    name               TEXT NOT NULL UNIQUE,
    url                TEXT NOT NULL,
    secret_ciphertext  TEXT NOT NULL,
    event_filter       TEXT NOT NULL DEFAULT '',
    allow_private      BOOLEAN NOT NULL DEFAULT FALSE,
    enabled            BOOLEAN NOT NULL DEFAULT TRUE,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE webhook_outbox (
    id              TEXT PRIMARY KEY,
    endpoint_id     TEXT NOT NULL REFERENCES webhook_endpoints(id) ON DELETE CASCADE,
    event_action    TEXT NOT NULL,
    payload         JSONB NOT NULL,
    attempt         INTEGER NOT NULL DEFAULT 0,
    next_attempt_at TIMESTAMPTZ NOT NULL,
    last_error      TEXT NOT NULL DEFAULT '',
    dead            BOOLEAN NOT NULL DEFAULT FALSE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    delivered_at    TIMESTAMPTZ
);

-- Worker's claim query orders by next_attempt_at ASC for live rows
-- (dead=false). Partial index keeps the hot scan small even with a
-- huge dead-letter tail.
CREATE INDEX idx_webhook_outbox_ready
    ON webhook_outbox (next_attempt_at)
    WHERE dead = FALSE AND delivered_at IS NULL;

-- +goose Down
-- intentionally empty (pre-release, no compatibility shim)
