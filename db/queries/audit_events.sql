-- R-Q-03: extend sqlc coverage to audit_events.

-- name: AppendAuditEvent :exec
INSERT INTO audit_events (id, actor_id, action, target_id, details, created_at)
VALUES ($1, $2, $3, $4, $5, $6);

-- name: ListAuditEvents :many
-- Returns the most recent N rows in chronological order. The inner
-- subquery makes the index do the heavy lifting (DESC + LIMIT) and the
-- outer ORDER BY restores ascending playback order so callers can
-- replay them as a timeline without an extra reverse pass.
SELECT id, actor_id, action, target_id, details, created_at
FROM (
    SELECT id, actor_id, action, target_id, details, created_at
    FROM audit_events
    ORDER BY created_at DESC, id DESC
    LIMIT $1
) sub
ORDER BY created_at, id;

-- name: PruneAuditEvents :execrows
DELETE FROM audit_events WHERE created_at < $1;
