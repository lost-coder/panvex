-- +goose Up
-- Q5.U-D-01: postgres REAL is 4-byte single precision; sqlite REAL is
-- 8-byte double precision. The drift means a counter that survives
-- 7 sig-digits round-trip on sqlite gets quantised to 4 bytes on
-- postgres. Promote every timeseries float column to DOUBLE PRECISION
-- so both backends carry the same numeric range. The migration is a
-- straight ALTER TYPE — postgres rewrites the table once, then both
-- drivers behave identically.
ALTER TABLE ts_server_load
    ALTER COLUMN cpu_pct_avg              TYPE DOUBLE PRECISION,
    ALTER COLUMN cpu_pct_max              TYPE DOUBLE PRECISION,
    ALTER COLUMN mem_pct_avg              TYPE DOUBLE PRECISION,
    ALTER COLUMN mem_pct_max              TYPE DOUBLE PRECISION,
    ALTER COLUMN disk_pct_avg             TYPE DOUBLE PRECISION,
    ALTER COLUMN disk_pct_max             TYPE DOUBLE PRECISION,
    ALTER COLUMN load_1m                  TYPE DOUBLE PRECISION,
    ALTER COLUMN load_5m                  TYPE DOUBLE PRECISION,
    ALTER COLUMN load_15m                 TYPE DOUBLE PRECISION,
    ALTER COLUMN dc_coverage_min_pct      TYPE DOUBLE PRECISION,
    ALTER COLUMN dc_coverage_avg_pct      TYPE DOUBLE PRECISION;

-- +goose Down
ALTER TABLE ts_server_load
    ALTER COLUMN cpu_pct_avg              TYPE REAL,
    ALTER COLUMN cpu_pct_max              TYPE REAL,
    ALTER COLUMN mem_pct_avg              TYPE REAL,
    ALTER COLUMN mem_pct_max              TYPE REAL,
    ALTER COLUMN disk_pct_avg             TYPE REAL,
    ALTER COLUMN disk_pct_max             TYPE REAL,
    ALTER COLUMN load_1m                  TYPE REAL,
    ALTER COLUMN load_5m                  TYPE REAL,
    ALTER COLUMN load_15m                 TYPE REAL,
    ALTER COLUMN dc_coverage_min_pct      TYPE REAL,
    ALTER COLUMN dc_coverage_avg_pct      TYPE REAL;
