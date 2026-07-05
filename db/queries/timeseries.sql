-- R-Q-03: timeseries tables (ts_server_load, ts_server_load_hourly,
-- ts_dc_health) — append-only metric history. Most operations are
-- bulk-write driven; this file covers the prune-only contract that
-- the retention worker calls. Detail-page chart reads stay in
-- internal/controlplane/storage/postgres/timeseries.go since they
-- have backend-specific filter shapes.

