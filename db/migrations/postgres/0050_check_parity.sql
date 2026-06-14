-- +goose Up
-- C1: Align enrollment_tokens.fleet_group_id FK with the SET NULL convention.
-- A group deleted out from under outstanding enrollment tokens should null their
-- scope, not block the delete (NO ACTION) and not silently vanish; this matches
-- the provider_id SET NULL convention and the documented enrollment design.
-- The two CHECK constraints (panel_settings.password_min_length and
-- agents.cert_spki_sha256) already exist in Postgres; no changes needed there.
ALTER TABLE enrollment_tokens DROP CONSTRAINT enrollment_tokens_fleet_group_id_fkey;
ALTER TABLE enrollment_tokens
    ADD CONSTRAINT enrollment_tokens_fleet_group_id_fkey
    FOREIGN KEY (fleet_group_id) REFERENCES fleet_groups (id) ON DELETE SET NULL;

-- +goose Down
ALTER TABLE enrollment_tokens DROP CONSTRAINT enrollment_tokens_fleet_group_id_fkey;
ALTER TABLE enrollment_tokens
    ADD CONSTRAINT enrollment_tokens_fleet_group_id_fkey
    FOREIGN KEY (fleet_group_id) REFERENCES fleet_groups (id);
