-- +goose Up
-- Invert the telemt reachability flag so proto3's bool default (false)
-- correctly represents the healthy/reachable case. Previously
-- telemt_reachable=1 meant healthy; now telemt_unreachable=0 means healthy.
ALTER TABLE telemt_runtime_current
    ADD COLUMN telemt_unreachable INTEGER NOT NULL DEFAULT 0;
UPDATE telemt_runtime_current
    SET telemt_unreachable = CASE WHEN telemt_reachable = 0 THEN 1 ELSE 0 END;
ALTER TABLE telemt_runtime_current DROP COLUMN telemt_reachable;

-- +goose Down
ALTER TABLE telemt_runtime_current
    ADD COLUMN telemt_reachable INTEGER NOT NULL DEFAULT 1;
UPDATE telemt_runtime_current
    SET telemt_reachable = CASE WHEN telemt_unreachable = 0 THEN 1 ELSE 0 END;
ALTER TABLE telemt_runtime_current DROP COLUMN telemt_unreachable;
