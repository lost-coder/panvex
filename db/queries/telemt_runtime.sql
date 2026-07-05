-- R-Q-03: telemt_runtime_current — minimal coverage so the table is
-- represented in db/queries. The full ~30-column shape stays inline
-- in internal/controlplane/storage/postgres/telemetry.go where the
-- bulk write helpers manage replace-by-agent semantics.

