-- +goose Up
-- S7: persist login-lockout state so a control-plane restart or
-- fail-over cannot be used to reset the failed-attempt counter for
-- an account. Key is the raw username. SQLite stores timestamps as
-- INTEGER unix seconds to match the rest of the schema (see sessions,
-- metric_snapshots, etc.).
CREATE TABLE IF NOT EXISTS login_lockouts (
    username         TEXT    PRIMARY KEY,
    failures         INTEGER NOT NULL DEFAULT 0,
    locked_at_unix   INTEGER,
    updated_at_unix  INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_login_lockouts_locked_at_unix
    ON login_lockouts(locked_at_unix);

-- +goose Down
DROP INDEX IF EXISTS idx_login_lockouts_locked_at_unix;
DROP TABLE IF EXISTS login_lockouts;
