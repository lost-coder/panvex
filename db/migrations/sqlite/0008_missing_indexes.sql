-- +goose Up
CREATE INDEX IF NOT EXISTS idx_jobs_status ON jobs (status);
CREATE INDEX IF NOT EXISTS idx_job_targets_agent_id ON job_targets (agent_id);
CREATE INDEX IF NOT EXISTS idx_metric_snapshots_captured_at ON metric_snapshots (captured_at_unix);
CREATE INDEX IF NOT EXISTS idx_enrollment_tokens_fleet_group_id ON enrollment_tokens (fleet_group_id);

-- +goose Down
DROP INDEX IF EXISTS idx_enrollment_tokens_fleet_group_id;
DROP INDEX IF EXISTS idx_metric_snapshots_captured_at;
DROP INDEX IF EXISTS idx_job_targets_agent_id;
DROP INDEX IF EXISTS idx_jobs_status;
