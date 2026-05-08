-- +goose Up
-- +goose NO TRANSACTION
-- 0038: tamper-evident audit chain.
-- Project is pre-release; existing rows keep the empty default and the
-- chain begins forming on the first post-migration write. The
-- verify-audit-chain subcommand treats consecutive empty hashes at
-- the head of the table as the legacy/genesis prefix.
--
-- NO TRANSACTION because CREATE INDEX CONCURRENTLY cannot run inside
-- a transaction. ADD COLUMN ... DEFAULT '' is metadata-only on
-- PostgreSQL 11+, so the lack of an outer transaction does not impede
-- atomicity in practice — the column is visible only after the
-- ALTER TABLE returns.

ALTER TABLE audit_events ADD COLUMN prev_hash  TEXT NOT NULL DEFAULT '';
ALTER TABLE audit_events ADD COLUMN event_hash TEXT NOT NULL DEFAULT '';

-- Helper index for the chain walker: the verifier reads in
-- (created_at, id) ascending order, which the existing primary key
-- only partially covers.
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_audit_events_chain_walk
    ON audit_events (created_at, id);

-- +goose Down
-- +goose NO TRANSACTION
-- intentionally empty (pre-release, no compatibility shim)
