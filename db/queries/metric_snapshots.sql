-- R-Q-03: metric_snapshots — historical agent metric blobs.

-- name: ListMetricSnapshots :many
SELECT id, agent_id, instance_id, captured_at, values
FROM metric_snapshots
ORDER BY captured_at, id;

-- name: AppendMetricSnapshot :exec
INSERT INTO metric_snapshots (id, agent_id, instance_id, captured_at, values)
VALUES ($1, $2, $3, $4, $5);

-- name: PruneMetricSnapshots :execrows
DELETE FROM metric_snapshots WHERE captured_at < $1;
