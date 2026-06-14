-- +goose Up
-- C1: Align SQLite schemas with Postgres.
--   1. panel_settings: add CHECK (password_min_length >= 8 AND password_min_length <= 128)
--   2. agents: add CHECK (length(cert_spki_sha256) IN (0, 32))
--   3. enrollment_tokens: add FOREIGN KEY (fleet_group_id) REFERENCES fleet_groups (id)
--        ON DELETE SET NULL — a group deleted out from under outstanding tokens should
--        null their scope, not block the delete (NO ACTION) and not silently vanish;
--        matches the provider_id SET NULL convention and the documented enrollment design.
-- SQLite cannot ALTER TABLE to add CHECK or FK constraints; each table must be rebuilt.

PRAGMA foreign_keys=OFF;

-- 1. panel_settings: add CHECK on password_min_length.
CREATE TABLE panel_settings_new (
    scope                 TEXT PRIMARY KEY,
    http_public_url       TEXT NOT NULL DEFAULT '',
    grpc_public_endpoint  TEXT NOT NULL DEFAULT '',
    password_min_length   INTEGER NOT NULL DEFAULT 10
        CHECK (password_min_length >= 8 AND password_min_length <= 128),
    retention_json        TEXT NOT NULL DEFAULT '',
    geoip_json            TEXT NOT NULL DEFAULT '',
    geoip_state_json      TEXT NOT NULL DEFAULT '',
    updated_at_unix       INTEGER NOT NULL
);
INSERT INTO panel_settings_new (scope, http_public_url, grpc_public_endpoint, password_min_length, retention_json, geoip_json, geoip_state_json, updated_at_unix)
    SELECT scope, http_public_url, grpc_public_endpoint, password_min_length, retention_json, geoip_json, geoip_state_json, updated_at_unix
    FROM panel_settings;
DROP TABLE panel_settings;
ALTER TABLE panel_settings_new RENAME TO panel_settings;
-- panel_settings has no non-PK indexes.

-- 2. agents: add CHECK (length(cert_spki_sha256) IN (0, 32)).
CREATE TABLE agents_new (
    id TEXT PRIMARY KEY,
    node_name TEXT NOT NULL,
    fleet_group_id TEXT,
    version TEXT NOT NULL DEFAULT '',
    read_only INTEGER NOT NULL DEFAULT 0,
    last_seen_at_unix INTEGER NOT NULL,
    created_at_unix INTEGER NOT NULL DEFAULT 0,
    cert_issued_at_unix INTEGER,
    cert_expires_at_unix INTEGER,
    cert_serial TEXT NOT NULL DEFAULT '',
    transport_mode TEXT NOT NULL DEFAULT 'inbound'
        CHECK (transport_mode IN ('inbound', 'outbound')),
    dial_address TEXT,
    bootstrap_state TEXT NOT NULL DEFAULT 'active'
        CHECK (bootstrap_state IN ('pending', 'active', 'expired', 'revoked')),
    bootstrap_token_hash BLOB,
    bootstrap_expires_at INTEGER,
    cert_spki_sha256 BLOB NOT NULL DEFAULT x''
        CHECK (length(cert_spki_sha256) IN (0, 32)),
    FOREIGN KEY (fleet_group_id) REFERENCES fleet_groups (id)
);
INSERT INTO agents_new (id, node_name, fleet_group_id, version, read_only, last_seen_at_unix, created_at_unix, cert_issued_at_unix, cert_expires_at_unix, cert_serial, transport_mode, dial_address, bootstrap_state, bootstrap_token_hash, bootstrap_expires_at, cert_spki_sha256)
    SELECT id, node_name, fleet_group_id, version, read_only, last_seen_at_unix, created_at_unix, cert_issued_at_unix, cert_expires_at_unix, cert_serial, transport_mode, dial_address, bootstrap_state, bootstrap_token_hash, bootstrap_expires_at, cert_spki_sha256
    FROM agents;
DROP TABLE agents;
ALTER TABLE agents_new RENAME TO agents;
-- Recreate all agents indexes.
CREATE INDEX idx_agents_cert_spki_sha256
    ON agents (cert_spki_sha256)
    WHERE length(cert_spki_sha256) > 0;
CREATE INDEX idx_agents_fleet_group_id ON agents (fleet_group_id);
CREATE INDEX idx_agents_last_seen_at ON agents (last_seen_at_unix);
CREATE INDEX idx_agents_transport_mode ON agents(transport_mode);

-- 3. enrollment_tokens: add FK ON DELETE SET NULL for fleet_group_id.
CREATE TABLE enrollment_tokens_new (
    value TEXT PRIMARY KEY,
    fleet_group_id TEXT,
    issued_at_unix INTEGER NOT NULL,
    expires_at_unix INTEGER NOT NULL,
    consumed_at_unix INTEGER,
    revoked_at_unix INTEGER,
    FOREIGN KEY (fleet_group_id) REFERENCES fleet_groups (id) ON DELETE SET NULL
);
INSERT INTO enrollment_tokens_new (value, fleet_group_id, issued_at_unix, expires_at_unix, consumed_at_unix, revoked_at_unix)
    SELECT value, fleet_group_id, issued_at_unix, expires_at_unix, consumed_at_unix, revoked_at_unix
    FROM enrollment_tokens;
DROP TABLE enrollment_tokens;
ALTER TABLE enrollment_tokens_new RENAME TO enrollment_tokens;
-- Recreate enrollment_tokens index.
CREATE INDEX idx_enrollment_tokens_fleet_group_id ON enrollment_tokens (fleet_group_id);

PRAGMA foreign_keys=ON;

-- +goose Down
-- Reverse: remove the added CHECKs and the enrollment_tokens FK.

PRAGMA foreign_keys=OFF;

-- 1. panel_settings: drop CHECK on password_min_length.
CREATE TABLE panel_settings_old (
    scope                 TEXT PRIMARY KEY,
    http_public_url       TEXT NOT NULL DEFAULT '',
    grpc_public_endpoint  TEXT NOT NULL DEFAULT '',
    password_min_length   INTEGER NOT NULL DEFAULT 10,
    retention_json        TEXT NOT NULL DEFAULT '',
    geoip_json            TEXT NOT NULL DEFAULT '',
    geoip_state_json      TEXT NOT NULL DEFAULT '',
    updated_at_unix       INTEGER NOT NULL
);
INSERT INTO panel_settings_old (scope, http_public_url, grpc_public_endpoint, password_min_length, retention_json, geoip_json, geoip_state_json, updated_at_unix)
    SELECT scope, http_public_url, grpc_public_endpoint, password_min_length, retention_json, geoip_json, geoip_state_json, updated_at_unix
    FROM panel_settings;
DROP TABLE panel_settings;
ALTER TABLE panel_settings_old RENAME TO panel_settings;

-- 2. agents: drop CHECK on cert_spki_sha256.
CREATE TABLE agents_old (
    id TEXT PRIMARY KEY,
    node_name TEXT NOT NULL,
    fleet_group_id TEXT,
    version TEXT NOT NULL DEFAULT '',
    read_only INTEGER NOT NULL DEFAULT 0,
    last_seen_at_unix INTEGER NOT NULL,
    created_at_unix INTEGER NOT NULL DEFAULT 0,
    cert_issued_at_unix INTEGER,
    cert_expires_at_unix INTEGER,
    cert_serial TEXT NOT NULL DEFAULT '',
    transport_mode TEXT NOT NULL DEFAULT 'inbound'
        CHECK (transport_mode IN ('inbound', 'outbound')),
    dial_address TEXT,
    bootstrap_state TEXT NOT NULL DEFAULT 'active'
        CHECK (bootstrap_state IN ('pending', 'active', 'expired', 'revoked')),
    bootstrap_token_hash BLOB,
    bootstrap_expires_at INTEGER,
    cert_spki_sha256 BLOB NOT NULL DEFAULT x'',
    FOREIGN KEY (fleet_group_id) REFERENCES fleet_groups (id)
);
INSERT INTO agents_old (id, node_name, fleet_group_id, version, read_only, last_seen_at_unix, created_at_unix, cert_issued_at_unix, cert_expires_at_unix, cert_serial, transport_mode, dial_address, bootstrap_state, bootstrap_token_hash, bootstrap_expires_at, cert_spki_sha256)
    SELECT id, node_name, fleet_group_id, version, read_only, last_seen_at_unix, created_at_unix, cert_issued_at_unix, cert_expires_at_unix, cert_serial, transport_mode, dial_address, bootstrap_state, bootstrap_token_hash, bootstrap_expires_at, cert_spki_sha256
    FROM agents;
DROP TABLE agents;
ALTER TABLE agents_old RENAME TO agents;
CREATE INDEX idx_agents_cert_spki_sha256
    ON agents (cert_spki_sha256)
    WHERE length(cert_spki_sha256) > 0;
CREATE INDEX idx_agents_fleet_group_id ON agents (fleet_group_id);
CREATE INDEX idx_agents_last_seen_at ON agents (last_seen_at_unix);
CREATE INDEX idx_agents_transport_mode ON agents(transport_mode);

-- 3. enrollment_tokens: remove FK.
CREATE TABLE enrollment_tokens_old (
    value TEXT PRIMARY KEY,
    fleet_group_id TEXT,
    issued_at_unix INTEGER NOT NULL,
    expires_at_unix INTEGER NOT NULL,
    consumed_at_unix INTEGER,
    revoked_at_unix INTEGER
);
INSERT INTO enrollment_tokens_old (value, fleet_group_id, issued_at_unix, expires_at_unix, consumed_at_unix, revoked_at_unix)
    SELECT value, fleet_group_id, issued_at_unix, expires_at_unix, consumed_at_unix, revoked_at_unix
    FROM enrollment_tokens;
DROP TABLE enrollment_tokens;
ALTER TABLE enrollment_tokens_old RENAME TO enrollment_tokens;
CREATE INDEX idx_enrollment_tokens_fleet_group_id ON enrollment_tokens (fleet_group_id);

PRAGMA foreign_keys=ON;
