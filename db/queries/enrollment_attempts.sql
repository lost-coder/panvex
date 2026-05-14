-- Phase 1 enrollment logging: typed query layer over the
-- enrollment_attempts and enrollment_events tables (migration 0041).
-- The recorder package in internal/controlplane/enrollmentlog calls
-- through these to record one attempt + N events per enrollment.

-- name: CreateEnrollmentAttempt :exec
INSERT INTO enrollment_attempts (
    id, token_id, agent_id, mode, client_addr, request_id, status, started_at
) VALUES ($1, $2, $3, $4, $5, $6, 'in_progress', $7);

-- name: AttachEnrollmentAttemptAgent :exec
UPDATE enrollment_attempts SET agent_id = $1 WHERE id = $2;

-- name: CompleteEnrollmentAttempt :execrows
-- Returns the number of rows affected so the Go adapter can report
-- whether the transition actually happened (idempotent finalize).
UPDATE enrollment_attempts
SET status = 'success', finished_at = $1
WHERE id = $2 AND status = 'in_progress';

-- name: FailEnrollmentAttempt :execrows
-- Returns the number of rows affected so the Go adapter can report
-- whether the transition actually happened (idempotent finalize).
UPDATE enrollment_attempts
SET status = 'failed', finished_at = $1, error_code = $2, error_message = $3
WHERE id = $4 AND status = 'in_progress';

-- name: GetEnrollmentAttempt :one
SELECT id, token_id, agent_id, mode, client_addr, request_id,
       status, error_code, error_message, started_at, finished_at
FROM enrollment_attempts WHERE id = $1;

-- name: ListEnrollmentAttempts :many
-- Optional filters use sqlc.narg so each parameter becomes nullable in
-- the generated Go struct. With sql_package: database/sql these surface
-- as sql.NullX / uuid.NullUUID wrappers; pass a zero-Valid value to
-- disable a filter.
SELECT id, token_id, agent_id, mode, client_addr, request_id,
       status, error_code, error_message, started_at, finished_at
FROM enrollment_attempts
WHERE (sqlc.narg('token_id')::uuid IS NULL OR token_id = sqlc.narg('token_id')::uuid)
  AND (sqlc.narg('agent_id')::uuid IS NULL OR agent_id = sqlc.narg('agent_id')::uuid)
  AND (sqlc.narg('status')::text   IS NULL OR status   = sqlc.narg('status')::text)
  AND (sqlc.narg('mode')::text     IS NULL OR mode     = sqlc.narg('mode')::text)
  AND (sqlc.narg('error_code')::text IS NULL OR error_code = sqlc.narg('error_code')::text)
  AND (sqlc.narg('started_after')::timestamptz  IS NULL OR started_at >= sqlc.narg('started_after')::timestamptz)
  AND (sqlc.narg('started_before')::timestamptz IS NULL OR started_at <  sqlc.narg('started_before')::timestamptz)
  AND (sqlc.narg('cursor_ts')::timestamptz IS NULL
       OR started_at < sqlc.narg('cursor_ts')::timestamptz
       OR (started_at = sqlc.narg('cursor_ts')::timestamptz
           AND id     < sqlc.narg('cursor_id')::uuid))
ORDER BY started_at DESC, id DESC
LIMIT sqlc.arg('limit');

-- name: AppendEnrollmentEvent :exec
INSERT INTO enrollment_events (attempt_id, ts, step, level, message, fields_json)
VALUES ($1, $2, $3, $4, $5, $6);

-- name: ListEnrollmentEvents :many
SELECT id, attempt_id, ts, step, level, message, fields_json
FROM enrollment_events
WHERE attempt_id = $1
ORDER BY ts ASC, id ASC;

-- name: DeleteOldEnrollmentAttempts :execrows
DELETE FROM enrollment_attempts WHERE started_at < $1;
