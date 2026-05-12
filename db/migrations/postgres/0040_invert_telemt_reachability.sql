-- +goose Up
-- +goose StatementBegin
-- Invert the telemt reachability flag so proto3's bool default (false)
-- correctly represents the healthy/reachable case. Previously
-- telemt_reachable=TRUE meant healthy; now telemt_unreachable=FALSE means healthy.
ALTER TABLE telemt_runtime_current
    ADD COLUMN telemt_unreachable BOOLEAN NOT NULL DEFAULT FALSE;
UPDATE telemt_runtime_current SET telemt_unreachable = NOT telemt_reachable;
ALTER TABLE telemt_runtime_current DROP COLUMN telemt_reachable;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE telemt_runtime_current
    ADD COLUMN telemt_reachable BOOLEAN NOT NULL DEFAULT TRUE;
UPDATE telemt_runtime_current SET telemt_reachable = NOT telemt_unreachable;
ALTER TABLE telemt_runtime_current DROP COLUMN telemt_unreachable;
-- +goose StatementEnd
