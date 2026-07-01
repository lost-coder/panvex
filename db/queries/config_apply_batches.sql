-- config_apply_batches / config_apply_batch_targets — group-wide config-apply
-- rollout batches and their per-agent delivery records. fleet_group_id is
-- bound as a real uuid.UUID param (see storage/postgres/config_apply_batches.go)
-- and read back cast to text so the storage layer stays a plain Go string on
-- both engines, mirroring user_fleet_group_scopes.sql.

-- name: InsertConfigApplyBatch :exec
INSERT INTO config_apply_batches
    (id, fleet_group_id, mode, wave_size, expected_revision, status, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8);

-- name: InsertConfigApplyBatchTarget :exec
INSERT INTO config_apply_batch_targets (batch_id, agent_id, wave_index, job_id, status)
VALUES ($1, $2, $3, $4, $5);

-- name: GetConfigApplyBatch :one
SELECT id, fleet_group_id::text AS fleet_group_id, mode, wave_size, expected_revision, status, created_at, updated_at
FROM config_apply_batches
WHERE id = $1;

-- name: ListConfigApplyBatchTargets :many
SELECT batch_id, agent_id, wave_index, job_id, status
FROM config_apply_batch_targets
WHERE batch_id = $1
ORDER BY wave_index ASC, agent_id ASC;

-- name: ListRunningConfigApplyBatches :many
SELECT id, fleet_group_id::text AS fleet_group_id, mode, wave_size, expected_revision, status, created_at, updated_at
FROM config_apply_batches
WHERE status = 'running'
ORDER BY created_at ASC, id ASC;

-- name: GetActiveConfigApplyBatchForGroup :one
SELECT id, fleet_group_id::text AS fleet_group_id, mode, wave_size, expected_revision, status, created_at, updated_at
FROM config_apply_batches
WHERE fleet_group_id = $1 AND status = 'running'
ORDER BY created_at ASC, id ASC
LIMIT 1;

-- name: UpdateConfigApplyBatchStatus :execrows
UPDATE config_apply_batches
SET status = $1, updated_at = $2
WHERE id = $3;

-- name: SetConfigApplyBatchTargetJob :exec
UPDATE config_apply_batch_targets
SET job_id = $1, status = $2
WHERE batch_id = $3 AND agent_id = $4;

-- name: UpdateConfigApplyBatchTargetStatus :exec
UPDATE config_apply_batch_targets
SET status = $1
WHERE batch_id = $2 AND agent_id = $3;

-- name: DeleteTerminalConfigApplyBatches :execrows
DELETE FROM config_apply_batches
WHERE status IN ('succeeded', 'failed', 'halted')
  AND updated_at < $1;
