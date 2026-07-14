-- +goose Up
-- R11: certificate-overlap window for agent credential rotation.
--
-- Issuing a new agent certificate used to overwrite the pinned serial and SPKI
-- immediately, while the agent only learns about the new certificate when the
-- renewal response reaches it. If the exchange was interrupted (panel restart,
-- stream drop), the two sides diverged: the panel expected the new credential,
-- the agent still held the old one, and the fail-closed verifier refused it.
-- An inbound agent could re-request a signature and self-heal; a listen-mode
-- node could not — it was stranded until an operator issued a recovery grant.
--
-- The panel now keeps the PREVIOUS credential valid alongside the new one for a
-- bounded window. The first connection presenting the new credential closes the
-- window. The set of simultaneously-valid credentials is therefore at most two,
-- and time-bounded; outside that pair the verifier stays fail-closed.

ALTER TABLE public.agents
    ADD COLUMN cert_serial_prev text DEFAULT ''::text NOT NULL,
    ADD COLUMN cert_spki_sha256_prev bytea DEFAULT '\x'::bytea NOT NULL,
    ADD COLUMN cert_overlap_until timestamp with time zone;

ALTER TABLE public.agents
    ADD CONSTRAINT agents_cert_spki_sha256_prev_check
    CHECK ((length(cert_spki_sha256_prev) = ANY (ARRAY[0, 32])));

-- +goose Down
ALTER TABLE public.agents
    DROP CONSTRAINT agents_cert_spki_sha256_prev_check;

ALTER TABLE public.agents
    DROP COLUMN cert_overlap_until,
    DROP COLUMN cert_spki_sha256_prev,
    DROP COLUMN cert_serial_prev;
