-- +goose Up
-- SQLite's matching migration is a no-op: 0026 in this repo already
-- ships the corrected enum (queued / sent / acknowledged / succeeded
-- / failed / expired). This file exists so the version numbers stay
-- aligned across the postgres + sqlite trees.
SELECT 1;

-- +goose Down
SELECT 1;
