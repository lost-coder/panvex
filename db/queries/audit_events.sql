-- R-Q-03: extend sqlc coverage to audit_events.

-- name: AppendAuditEvent :exec
INSERT INTO audit_events (id, actor_id, action, target_id, details, created_at, prev_hash, event_hash)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8);

-- name: ListAuditEvents :many
-- Returns the most recent N rows in chronological order. The inner
-- subquery makes the index do the heavy lifting (DESC + LIMIT) and the
-- outer ORDER BY restores ascending playback order so callers can
-- replay them as a timeline without an extra reverse pass.
SELECT id, actor_id, action, target_id, details, created_at, prev_hash, event_hash
FROM (
    SELECT id, actor_id, action, target_id, details, created_at, prev_hash, event_hash
    FROM audit_events
    ORDER BY created_at DESC, id DESC
    LIMIT $1
) sub
ORDER BY created_at, id;

-- name: LatestAuditChainHash :one
-- Returns the event_hash of the most recently persisted audit row,
-- or empty string when the table is empty. The batch writer reads
-- this once before each flush and chains every row in the batch
-- onto the tail of the existing chain.
--
-- The CAST forces sqlc to infer a `string` return rather than the
-- generic interface{} that COALESCE would otherwise produce — the
-- column is TEXT NOT NULL in both backends, so the cast is a no-op
-- at the SQL layer.
SELECT CAST(COALESCE(
    (SELECT event_hash
       FROM audit_events
       ORDER BY created_at DESC, id DESC
       LIMIT 1),
    ''
) AS TEXT) AS hash;

-- name: PruneAuditEvents :execrows
DELETE FROM audit_events WHERE created_at < $1;
