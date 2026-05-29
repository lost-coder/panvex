-- +goose Up
-- +goose StatementBegin
-- IN-M2: operator-facing diagnostic stamped on a successful (non-delete)
-- apply when the node returned no connection links. In that case the
-- existing connection_links are kept but may be stale after a host or
-- secret change; this column explains why so the dashboard can flag the
-- link instead of silently serving it. Empty string means "no issue".
ALTER TABLE client_deployments
    ADD COLUMN link_diagnostic TEXT NOT NULL DEFAULT '';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE client_deployments DROP COLUMN IF EXISTS link_diagnostic;
-- +goose StatementEnd
