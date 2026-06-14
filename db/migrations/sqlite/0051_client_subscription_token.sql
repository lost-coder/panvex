-- +goose Up
-- See postgres/0051 for rationale. SQLite UNIQUE indexes treat NULLs as
-- distinct, so multiple NULL tokens coexist without a partial predicate.
ALTER TABLE clients ADD COLUMN subscription_token TEXT;
CREATE UNIQUE INDEX IF NOT EXISTS clients_subscription_token_key
    ON clients (subscription_token);

-- +goose Down
DROP INDEX IF EXISTS clients_subscription_token_key;
ALTER TABLE clients DROP COLUMN subscription_token;
