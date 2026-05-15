-- +goose Up
-- +goose StatementBegin
-- Phase 3: per-(client, agent) record of when the panel last applied a
-- successful quota reset. Compared against Telemt's reported
-- last_reset_epoch_secs on each ClientUsage snapshot to detect drift —
-- i.e. cases where the panel-driven reset job succeeded but Telemt's
-- persisted quota state has fallen behind (Telemt restart before
-- sidecar flush, raw curl reset out of band, etc.).
ALTER TABLE client_deployments
    ADD COLUMN last_reset_epoch_secs BIGINT NOT NULL DEFAULT 0;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE client_deployments DROP COLUMN IF EXISTS last_reset_epoch_secs;
-- +goose StatementEnd
