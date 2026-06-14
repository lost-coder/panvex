-- +goose Up
-- +goose NO TRANSACTION
-- subscription_token is the opaque, unguessable handle embedded in a client's
-- public subscription URL (/sub/<token>). Nullable: legacy rows get a token
-- when the operator rotates (no backfill — pre-prod). UNIQUE so a token maps
-- to at most one client; the partial predicate lets multiple rows stay NULL.
--
-- NO TRANSACTION pragma: required because CREATE/DROP INDEX CONCURRENTLY
-- cannot run inside a transaction. ALTER TABLE ADD COLUMN takes an ACCESS
-- EXCLUSIVE lock briefly, but the column has no DEFAULT so PG skips the
-- table rewrite.
ALTER TABLE clients ADD COLUMN subscription_token TEXT;
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS clients_subscription_token_key
    ON clients (subscription_token)
    WHERE subscription_token IS NOT NULL;

-- +goose Down
DROP INDEX CONCURRENTLY IF EXISTS clients_subscription_token_key;
ALTER TABLE clients DROP COLUMN subscription_token;
