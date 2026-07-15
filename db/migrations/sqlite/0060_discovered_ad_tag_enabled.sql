-- +goose Up
-- Wire-audit I4: the agent reports user_ad_tag + enabled for every
-- discovered client, but the panel dropped both. The adopt path then
-- hardcoded enabled=true with an empty ad-tag, so the first post-adopt
-- edit PATCHed user_ad_tag:null to the node and wiped the tag. Mirror of
-- db/migrations/postgres/0060_discovered_ad_tag_enabled.sql. Plain ADD
-- COLUMNs: SQLite handles these without a table rebuild. enabled defaults
-- to 1 (true) — pre-migration rows were discovered while the flow ignored
-- the flag, and an active Telemt client is enabled unless reported otherwise.

ALTER TABLE discovered_clients ADD COLUMN user_ad_tag TEXT NOT NULL DEFAULT '';
ALTER TABLE discovered_clients ADD COLUMN enabled INTEGER NOT NULL DEFAULT 1;

-- +goose Down
ALTER TABLE discovered_clients DROP COLUMN enabled;
ALTER TABLE discovered_clients DROP COLUMN user_ad_tag;
