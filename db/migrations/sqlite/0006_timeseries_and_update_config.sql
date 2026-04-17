-- +goose Up
-- update_config stores internal feature-flag/kv state for the updater subsystem.
CREATE TABLE IF NOT EXISTS update_config (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS ts_server_load (
    agent_id                TEXT NOT NULL,
    captured_at_unix        INTEGER NOT NULL,
    cpu_pct_avg             REAL NOT NULL DEFAULT 0,
    cpu_pct_max             REAL NOT NULL DEFAULT 0,
    mem_pct_avg             REAL NOT NULL DEFAULT 0,
    mem_pct_max             REAL NOT NULL DEFAULT 0,
    disk_pct_avg            REAL NOT NULL DEFAULT 0,
    disk_pct_max            REAL NOT NULL DEFAULT 0,
    load_1m                 REAL NOT NULL DEFAULT 0,
    load_5m                 REAL NOT NULL DEFAULT 0,
    load_15m                REAL NOT NULL DEFAULT 0,
    connections_avg         INTEGER NOT NULL DEFAULT 0,
    connections_max         INTEGER NOT NULL DEFAULT 0,
    connections_me_avg      INTEGER NOT NULL DEFAULT 0,
    connections_direct_avg  INTEGER NOT NULL DEFAULT 0,
    active_users_avg        INTEGER NOT NULL DEFAULT 0,
    active_users_max        INTEGER NOT NULL DEFAULT 0,
    connections_total       INTEGER NOT NULL DEFAULT 0,
    connections_bad_total   INTEGER NOT NULL DEFAULT 0,
    handshake_timeouts_total INTEGER NOT NULL DEFAULT 0,
    dc_coverage_min_pct     REAL NOT NULL DEFAULT 0,
    dc_coverage_avg_pct     REAL NOT NULL DEFAULT 0,
    healthy_upstreams       INTEGER NOT NULL DEFAULT 0,
    total_upstreams         INTEGER NOT NULL DEFAULT 0,
    net_bytes_sent          INTEGER NOT NULL DEFAULT 0,
    net_bytes_recv          INTEGER NOT NULL DEFAULT 0,
    sample_count            INTEGER NOT NULL DEFAULT 1,
    PRIMARY KEY (agent_id, captured_at_unix)
);

CREATE TABLE IF NOT EXISTS ts_dc_health (
    agent_id         TEXT NOT NULL,
    captured_at_unix INTEGER NOT NULL,
    dc               INTEGER NOT NULL,
    coverage_pct_avg REAL NOT NULL DEFAULT 0,
    coverage_pct_min REAL NOT NULL DEFAULT 0,
    rtt_ms_avg       REAL NOT NULL DEFAULT 0,
    rtt_ms_max       REAL NOT NULL DEFAULT 0,
    alive_writers_min INTEGER NOT NULL DEFAULT 0,
    required_writers INTEGER NOT NULL DEFAULT 0,
    load_max         INTEGER NOT NULL DEFAULT 0,
    sample_count     INTEGER NOT NULL DEFAULT 1,
    PRIMARY KEY (agent_id, dc, captured_at_unix)
);

CREATE TABLE IF NOT EXISTS ts_server_load_hourly (
    agent_id          TEXT NOT NULL,
    bucket_hour_unix  INTEGER NOT NULL,
    cpu_pct_avg       REAL,
    cpu_pct_max       REAL,
    mem_pct_avg       REAL,
    mem_pct_max       REAL,
    connections_avg   REAL,
    connections_max   INTEGER,
    active_users_avg  REAL,
    active_users_max  INTEGER,
    dc_coverage_min   REAL,
    dc_coverage_avg   REAL,
    sample_count      INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (agent_id, bucket_hour_unix)
);

CREATE TABLE IF NOT EXISTS client_ip_history (
    agent_id    TEXT NOT NULL,
    client_id   TEXT NOT NULL,
    ip_address  TEXT NOT NULL,
    first_seen_unix INTEGER NOT NULL,
    last_seen_unix  INTEGER NOT NULL,
    PRIMARY KEY (agent_id, client_id, ip_address)
);

-- +goose Down
DROP TABLE IF EXISTS client_ip_history;
DROP TABLE IF EXISTS ts_server_load_hourly;
DROP TABLE IF EXISTS ts_dc_health;
DROP TABLE IF EXISTS ts_server_load;
DROP TABLE IF EXISTS update_config;
