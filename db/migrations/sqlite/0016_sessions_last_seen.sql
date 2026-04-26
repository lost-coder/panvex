-- +goose Up
ALTER TABLE sessions ADD COLUMN last_seen_at_unix INTEGER NOT NULL DEFAULT 0;
UPDATE sessions SET last_seen_at_unix = created_at_unix WHERE last_seen_at_unix = 0;

-- +goose Down
-- SQLite cannot DROP COLUMN inline (older versions); rebuild table.
CREATE TABLE sessions_old (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL,
    created_at_unix INTEGER NOT NULL,
    FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE
);
INSERT INTO sessions_old (id, user_id, created_at_unix)
SELECT id, user_id, created_at_unix FROM sessions;
DROP TABLE sessions;
ALTER TABLE sessions_old RENAME TO sessions;
CREATE INDEX IF NOT EXISTS idx_sessions_user_id ON sessions(user_id);
CREATE INDEX IF NOT EXISTS idx_sessions_created_at_unix ON sessions(created_at_unix);
