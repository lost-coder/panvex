-- +goose NO TRANSACTION
-- These indexes target tables already populated in production
-- (jobs, job_targets, metric_snapshots, enrollment_tokens), so each
-- CREATE INDEX must run CONCURRENTLY to avoid an ACCESS EXCLUSIVE lock
-- on the table for the duration of the build. CONCURRENTLY is illegal
-- inside a transaction, hence the NO TRANSACTION pragma above.
-- +goose Up
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_jobs_status ON jobs (status);
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_job_targets_agent_id ON job_targets (agent_id);
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_metric_snapshots_captured_at ON metric_snapshots (captured_at);
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_enrollment_tokens_fleet_group_id ON enrollment_tokens (fleet_group_id);

-- +goose Down
DROP INDEX CONCURRENTLY IF EXISTS idx_enrollment_tokens_fleet_group_id;
DROP INDEX CONCURRENTLY IF EXISTS idx_metric_snapshots_captured_at;
DROP INDEX CONCURRENTLY IF EXISTS idx_job_targets_agent_id;
DROP INDEX CONCURRENTLY IF EXISTS idx_jobs_status;
