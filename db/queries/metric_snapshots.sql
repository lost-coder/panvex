-- R-Q-03: metric_snapshots — historical agent metric blobs.

-- name: ListMetricSnapshots :many
-- M4: capped at 512 rows (keeping the newest 512, returned oldest→newest) to
-- match the hand-written storage.Store implementations
-- (internal/controlplane/storage/sqlite/metrics.go,
-- internal/controlplane/storage/postgres/metrics.go) that actually serve
-- this query at runtime. This sqlc-generated query is not currently wired
-- into storage.Store, but it must not silently diverge from the documented
-- 512-row contract (http_inventory.go) if it is ever used directly via
-- Store.Queries().
SELECT id, agent_id, instance_id, captured_at, values FROM (
  SELECT id, agent_id, instance_id, captured_at, values
  FROM metric_snapshots
  ORDER BY captured_at DESC, id DESC
  LIMIT 512
) capped
ORDER BY captured_at ASC, id ASC;

-- name: AppendMetricSnapshot :exec
INSERT INTO metric_snapshots (id, agent_id, instance_id, captured_at, values)
VALUES ($1, $2, $3, $4, $5);

-- name: PruneMetricSnapshots :execrows
DELETE FROM metric_snapshots WHERE captured_at < $1;
