-- +goose Up
-- 0038: tamper-evident audit chain.
-- Mirror of the postgres 0038 migration. Pre-release: existing rows
-- carry the empty default; the chain begins forming on the first
-- post-migration write.
--
-- SQLite cannot add multiple columns in one ALTER TABLE statement,
-- so we issue two separate ALTERs. This is a no-op rewrite under
-- modernc.org/sqlite (column adds are O(1) for non-PRIMARY KEY columns).

ALTER TABLE audit_events ADD COLUMN prev_hash  TEXT NOT NULL DEFAULT '';
ALTER TABLE audit_events ADD COLUMN event_hash TEXT NOT NULL DEFAULT '';

-- Helper index for the chain walker. SQLite's audit_events uses
-- created_at_unix (the postgres companion stores TIMESTAMPTZ), so the
-- index columns differ; the verifier reads in (timestamp, id)
-- ascending order and the planner picks this index for both forms.
CREATE INDEX IF NOT EXISTS idx_audit_events_chain_walk
    ON audit_events (created_at_unix, id);

-- +goose Down
-- intentionally empty (pre-release, no compatibility shim)
