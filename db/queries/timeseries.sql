-- R-Q-03: timeseries tables (ts_server_load, ts_server_load_hourly,
-- ts_dc_health) — append-only metric history. Most operations are
-- bulk-write driven; this file covers the prune-only contract that
-- the retention worker calls. Detail-page chart reads stay in
-- internal/controlplane/storage/postgres/timeseries.go since they
-- have backend-specific filter shapes.

-- name: PruneServerLoadPoints :execrows
DELETE FROM ts_server_load WHERE captured_at < $1;

-- name: PruneServerLoadHourly :execrows
DELETE FROM ts_server_load_hourly WHERE bucket_hour < $1;

-- name: PruneDCHealthPoints :execrows
DELETE FROM ts_dc_health WHERE captured_at < $1;
