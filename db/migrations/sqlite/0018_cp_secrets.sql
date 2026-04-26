-- +goose Up
CREATE TABLE IF NOT EXISTS cp_secrets (
    key TEXT PRIMARY KEY,
    value BLOB NOT NULL,
    updated_at_unix INTEGER NOT NULL
);

-- +goose Down
DROP TABLE IF EXISTS cp_secrets;
