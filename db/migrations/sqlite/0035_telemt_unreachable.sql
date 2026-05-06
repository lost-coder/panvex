-- +goose Up
ALTER TABLE telemt_runtime_current
    ADD COLUMN telemt_reachable INTEGER NOT NULL DEFAULT 1;
ALTER TABLE telemt_runtime_current
    ADD COLUMN telemt_unreachable_since_unix INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE telemt_runtime_current DROP COLUMN telemt_unreachable_since_unix;
ALTER TABLE telemt_runtime_current DROP COLUMN telemt_reachable;
