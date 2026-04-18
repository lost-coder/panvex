-- +goose Up
-- S7: persist login-lockout state so a control-plane restart or
-- fail-over cannot be used to reset the failed-attempt counter for
-- an account. Key is the raw username as supplied by the client:
-- the auth flow already normalises it (strings.ToLower / TrimSpace),
-- and the DB-level PK makes upserts serializable without a
-- separate unique index.
CREATE TABLE IF NOT EXISTS login_lockouts (
    username       TEXT        PRIMARY KEY,
    failures       INTEGER     NOT NULL DEFAULT 0,
    locked_at      TIMESTAMPTZ,
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_login_lockouts_locked_at
    ON login_lockouts(locked_at);

-- +goose Down
DROP INDEX IF EXISTS idx_login_lockouts_locked_at;
DROP TABLE IF EXISTS login_lockouts;
