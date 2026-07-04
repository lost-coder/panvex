-- +goose Up
-- P3-3.4 (аудит #25a): одиночный config-apply становится batch-of-one без
-- fleet-group-скоупа. NULL fleet_group_id = agent-scoped батч; групповой
-- active-lookup (WHERE fleet_group_id = $1) такие батчи не видит.
ALTER TABLE config_apply_batches ALTER COLUMN fleet_group_id DROP NOT NULL;

-- +goose Down
SELECT 1;
