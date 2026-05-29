-- +goose Up
-- +goose StatementBegin
-- IN-H2: persist the per-(client, agent) quota counters alongside the other
-- usage fields. Previously quota_used_bytes / quota_last_reset_unix lived
-- only in the in-memory mirror, so a panel restart snapped them to 0 until
-- the next agent usage tick repopulated them.
ALTER TABLE client_usage
    ADD COLUMN quota_used_bytes BIGINT NOT NULL DEFAULT 0;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE client_usage
    ADD COLUMN quota_last_reset_unix BIGINT NOT NULL DEFAULT 0;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE client_usage DROP COLUMN IF EXISTS quota_last_reset_unix;
-- +goose StatementEnd
-- +goose StatementBegin
ALTER TABLE client_usage DROP COLUMN IF EXISTS quota_used_bytes;
-- +goose StatementEnd
