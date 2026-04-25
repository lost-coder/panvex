-- +goose NO TRANSACTION
-- +goose Up
-- P2-LOG-02 (L-10 / M-C4): belt-and-suspenders guard against duplicate
-- pending_review rows. The table already has UNIQUE (agent_id, client_name)
-- from 0002, but adding this partial UNIQUE index makes the dedupe intent
-- explicit and keeps the invariant intact even if the broader constraint
-- is ever loosened (e.g. to allow historical "ignored"/"adopted" copies).
--
-- discovered_clients is a populated table in production, so the build
-- runs CONCURRENTLY (and therefore outside any transaction) to avoid
-- locking writers for the duration.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_discovered_clients_pending_unique
    ON discovered_clients (agent_id, client_name)
    WHERE status = 'pending_review';

-- +goose Down
DROP INDEX CONCURRENTLY IF EXISTS idx_discovered_clients_pending_unique;
