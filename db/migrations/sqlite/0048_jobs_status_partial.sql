-- +goose Up
-- +goose NO TRANSACTION
-- F2: deriveJobStatus now emits a new terminal job status, "partial", for a
-- MIXED outcome (at least one target succeeded AND at least one ended
-- terminally-unsuccessful). The jobs.status CHECK constraint (born in 0026 /
-- baseline) only allowed queued/running/succeeded/failed/expired, so the very
-- first attempt to persist a partial job tripped "CHECK constraint failed:
-- status IN (...)" and the write was silently dropped — the restored job
-- reverted to its last-valid persisted status (running, or expired after TTL).
--
-- SQLite has no ALTER TABLE ... ADD/DROP CONSTRAINT, so rebuild the jobs table
-- with the widened enum using the rename → recreate → copy → drop → restore
-- pattern from 0026. job_targets is untouched: target rows never take the
-- "partial" value (it is a job-level rollup only).

PRAGMA foreign_keys = OFF;

CREATE TABLE jobs_new (
    id TEXT PRIMARY KEY,
    action TEXT NOT NULL,
    actor_id TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('queued','running','succeeded','failed','expired','partial')),
    created_at_unix INTEGER NOT NULL,
    ttl_nanos INTEGER NOT NULL,
    idempotency_key TEXT NOT NULL UNIQUE,
    payload_json TEXT NOT NULL DEFAULT ''
);

INSERT INTO jobs_new (id, action, actor_id, status, created_at_unix, ttl_nanos, idempotency_key, payload_json)
SELECT id, action, actor_id, status, created_at_unix, ttl_nanos, idempotency_key, payload_json FROM jobs;

DROP TABLE jobs;
ALTER TABLE jobs_new RENAME TO jobs;

CREATE INDEX IF NOT EXISTS idx_jobs_created_at ON jobs (created_at_unix);
CREATE INDEX IF NOT EXISTS idx_jobs_status ON jobs (status);
CREATE INDEX IF NOT EXISTS idx_jobs_actor_id ON jobs (actor_id);

PRAGMA foreign_keys = ON;

-- +goose Down
SELECT 1;
