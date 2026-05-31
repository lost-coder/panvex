-- +goose Up
-- F2: deriveJobStatus now emits a new terminal job status, "partial", for a
-- MIXED outcome (at least one target succeeded AND at least one ended
-- terminally-unsuccessful). The jobs_status_check constraint added in 0023
-- only allowed queued/running/succeeded/failed/expired, so persisting a
-- partial job tripped the CHECK and the write was dropped. Widen the enum.
ALTER TABLE jobs DROP CONSTRAINT IF EXISTS jobs_status_check;
ALTER TABLE jobs ADD CONSTRAINT jobs_status_check
    CHECK (status IN ('queued','running','succeeded','failed','expired','partial'));

-- +goose Down
ALTER TABLE jobs DROP CONSTRAINT IF EXISTS jobs_status_check;
ALTER TABLE jobs ADD CONSTRAINT jobs_status_check
    CHECK (status IN ('queued','running','succeeded','failed','expired'));
