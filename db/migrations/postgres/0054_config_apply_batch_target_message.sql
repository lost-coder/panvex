-- +goose Up
-- +goose StatementBegin
-- Persists a failed (or otherwise terminal) target's message so the
-- resumable batch-status view (GET /fleet-groups/{id}/config/apply/batches/{batchId})
-- can surface the failure reason after the underlying config.apply job has
-- been evicted from the in-memory jobs store. Previously the message only
-- lived on the live jobs.Job target (ResultText), which is lost once the
-- job rolls off — defeating the point of a persistent, resumable batch.
ALTER TABLE config_apply_batch_targets
    ADD COLUMN message TEXT NOT NULL DEFAULT '';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE config_apply_batch_targets DROP COLUMN IF EXISTS message;
-- +goose StatementEnd
