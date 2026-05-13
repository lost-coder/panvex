-- +goose Up
CREATE TABLE enrollment_attempts (
    id              TEXT PRIMARY KEY,
    token_id        TEXT,
    agent_id        TEXT,
    mode            TEXT NOT NULL CHECK (mode IN ('inbound', 'outbound')),
    client_addr     TEXT,
    request_id      TEXT NOT NULL,
    status          TEXT NOT NULL CHECK (status IN ('in_progress', 'success', 'failed')),
    error_code      TEXT,
    error_message   TEXT,
    started_at      TIMESTAMP NOT NULL,
    finished_at     TIMESTAMP
);

CREATE INDEX idx_enrollment_attempts_token   ON enrollment_attempts(token_id);
CREATE INDEX idx_enrollment_attempts_agent   ON enrollment_attempts(agent_id);
CREATE INDEX idx_enrollment_attempts_started ON enrollment_attempts(started_at);

CREATE TABLE enrollment_events (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    attempt_id  TEXT NOT NULL REFERENCES enrollment_attempts(id) ON DELETE CASCADE,
    ts          TIMESTAMP NOT NULL,
    step        TEXT NOT NULL,
    level       TEXT NOT NULL CHECK (level IN ('info', 'warn', 'error')),
    message     TEXT,
    fields_json TEXT
);

CREATE INDEX idx_enrollment_events_attempt ON enrollment_events(attempt_id, ts);

-- +goose Down
DROP INDEX IF EXISTS idx_enrollment_events_attempt;
DROP TABLE IF EXISTS enrollment_events;
DROP INDEX IF EXISTS idx_enrollment_attempts_started;
DROP INDEX IF EXISTS idx_enrollment_attempts_agent;
DROP INDEX IF EXISTS idx_enrollment_attempts_token;
DROP TABLE IF EXISTS enrollment_attempts;
