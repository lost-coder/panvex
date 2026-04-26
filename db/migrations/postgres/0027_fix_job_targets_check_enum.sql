-- +goose Up
-- Migration 0023 added a CHECK constraint on job_targets.status with
-- the value `dispatched` — but the application enum
-- (internal/controlplane/jobs.TargetStatus) is `sent`, not
-- `dispatched`. Production writes to target.status="sent" trip the
-- constraint and bubble up as "constraint failed: CHECK constraint
-- failed" from the persist loop, leaving the row at its previous
-- value. The SQLite contract test
-- TestServiceUpdateTargetPersistsLatestVersionAfterOutOfOrderWrites
-- caught it once SQLite's mirror constraint landed in 0026.
--
-- Drop the wrong constraint and re-add it with the correct enum.
ALTER TABLE job_targets DROP CONSTRAINT IF EXISTS job_targets_status_check;
ALTER TABLE job_targets ADD CONSTRAINT job_targets_status_check
    CHECK (status IN ('queued','sent','acknowledged','succeeded','failed','expired'));

-- +goose Down
ALTER TABLE job_targets DROP CONSTRAINT IF EXISTS job_targets_status_check;
ALTER TABLE job_targets ADD CONSTRAINT job_targets_status_check
    CHECK (status IN ('queued','dispatched','acknowledged','succeeded','failed','expired'));
