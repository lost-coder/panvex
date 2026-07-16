-- +goose Up
-- Wire-audit I4: the agent reports user_ad_tag + enabled for every
-- discovered client (gatewayrpc.ClientDetailRecord fields 13/14), but the
-- panel dropped both on ingest. The adopt path then hardcoded enabled=true
-- with an empty ad-tag, so the first post-adopt client edit PATCHed
-- user_ad_tag:null (JSON-merge-patch Remove) to the node and wiped the tag.
-- Persist both so discovery round-trips the node's real state into adopt.
-- enabled defaults to TRUE — pre-migration rows were discovered while the
-- flow ignored the flag, and an active Telemt client is enabled unless
-- reported otherwise.

ALTER TABLE public.discovered_clients
    ADD COLUMN user_ad_tag text DEFAULT ''::text NOT NULL,
    ADD COLUMN enabled boolean DEFAULT true NOT NULL;

-- +goose Down
ALTER TABLE public.discovered_clients
    DROP COLUMN enabled,
    DROP COLUMN user_ad_tag;
