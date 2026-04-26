-- +goose Up
CREATE INDEX IF NOT EXISTS idx_jobs_actor_id ON jobs(actor_id);

-- +goose Down
DROP INDEX IF EXISTS idx_jobs_actor_id;
