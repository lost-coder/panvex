-- +goose Up
-- +goose StatementBegin
ALTER TABLE telemt_runtime_current
    ADD COLUMN telemt_reachable BOOLEAN NOT NULL DEFAULT TRUE,
    ADD COLUMN telemt_unreachable_since_unix BIGINT NOT NULL DEFAULT 0;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE telemt_runtime_current
    DROP COLUMN IF EXISTS telemt_unreachable_since_unix,
    DROP COLUMN IF EXISTS telemt_reachable;
-- +goose StatementEnd
