-- +goose Up
-- Q4.U-D-02: explicit CHECK constraints on enum-shaped TEXT columns.
-- These columns are already enums in the application — adding the
-- check at the DB level catches a stray writer (e.g. a future sqlc
-- query bug, an admin-issued UPDATE) before it corrupts the row.
ALTER TABLE jobs ADD CONSTRAINT jobs_status_check
    CHECK (status IN ('queued','running','succeeded','failed','expired'));

ALTER TABLE job_targets ADD CONSTRAINT job_targets_status_check
    CHECK (status IN ('queued','dispatched','acknowledged','succeeded','failed','expired'));

ALTER TABLE discovered_clients ADD CONSTRAINT discovered_clients_status_check
    CHECK (status IN ('pending_review','adopted','ignored'));

-- +goose Down
ALTER TABLE jobs DROP CONSTRAINT IF EXISTS jobs_status_check;
ALTER TABLE job_targets DROP CONSTRAINT IF EXISTS job_targets_status_check;
ALTER TABLE discovered_clients DROP CONSTRAINT IF EXISTS discovered_clients_status_check;
