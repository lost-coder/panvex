-- +goose Up
-- P2-DB-05 (DF-25 / M-F13): rename SQLite columns to match the PostgreSQL
-- schema. Historically SQLite used `details_json` and `values_json` (TEXT)
-- while Postgres used the unsuffixed `details` and `values` (JSONB) names.
-- That divergence forced every Store method to keep two SQL variants — one
-- per backend — and any edit had to touch both. Converging on the Postgres
-- names lets the storage layer share statements.
--
-- SQLite supports ALTER TABLE ... RENAME COLUMN since 3.25 (2018). The
-- modernc.org/sqlite driver we embed is based on 3.45+, so direct RENAME
-- COLUMN works — no table-rebuild fallback needed.
--
-- `values` is a reserved keyword in SQLite, so any subsequent query that
-- refers to it must double-quote the identifier. That change lives in
-- internal/controlplane/storage/sqlite/store.go alongside this migration.
ALTER TABLE audit_events RENAME COLUMN details_json TO details;
ALTER TABLE metric_snapshots RENAME COLUMN values_json TO "values";

-- +goose Down
ALTER TABLE audit_events RENAME COLUMN details TO details_json;
ALTER TABLE metric_snapshots RENAME COLUMN "values" TO values_json;
