-- +goose Up
-- SQLite mirror of postgres/0054_config_apply_batch_target_message.sql.
-- Persists a failed (or otherwise terminal) target's message so the
-- resumable batch-status view (GET /fleet-groups/{id}/config/apply/batches/{batchId})
-- can surface the failure reason after the underlying config.apply job has
-- been evicted from the in-memory jobs store. Adding a NOT NULL column with
-- a DEFAULT is a plain ALTER TABLE on SQLite (no CHECK/FK added), so no
-- table rebuild is required.
ALTER TABLE config_apply_batch_targets
    ADD COLUMN message TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE config_apply_batch_targets DROP COLUMN message;
