-- +goose Up
-- P9 squash (2026-07): консолидация миграций 0001..0058 в один init.
-- Файл сгенерирован pg_dump --schema-only с БД, смигрированной полным
-- историческим деревом (задача P9-4). НЕ редактировать задним числом:
-- изменения схемы = новая миграция с номером >= 0059 (README рядом).


CREATE TABLE public.agent_certificate_recovery_grants (
    agent_id text NOT NULL,
    issued_by text NOT NULL,
    issued_at timestamp with time zone NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    used_at timestamp with time zone,
    revoked_at timestamp with time zone
);

CREATE TABLE public.agent_config_targets (
    scope_type text NOT NULL,
    scope_id text NOT NULL,
    sections_json text DEFAULT '{}'::text NOT NULL,
    created_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone NOT NULL
);

CREATE TABLE public.agent_fallback_state (
    agent_id text NOT NULL,
    entered_at_unix bigint NOT NULL
);

CREATE TABLE public.agent_revocations (
    agent_id text NOT NULL,
    revoked_at timestamp with time zone DEFAULT now() NOT NULL,
    cert_expires_at timestamp with time zone NOT NULL
);

CREATE TABLE public.agents (
    id text NOT NULL,
    node_name text NOT NULL,
    fleet_group_id uuid,
    version text DEFAULT ''::text NOT NULL,
    read_only boolean DEFAULT false NOT NULL,
    last_seen_at timestamp with time zone NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    cert_issued_at timestamp with time zone,
    cert_expires_at timestamp with time zone,
    cert_serial text DEFAULT ''::text NOT NULL,
    transport_mode text DEFAULT 'inbound'::text NOT NULL,
    dial_address text,
    bootstrap_state text DEFAULT 'active'::text NOT NULL,
    bootstrap_token_hash bytea,
    bootstrap_expires_at timestamp with time zone,
    cert_spki_sha256 bytea DEFAULT '\x'::bytea NOT NULL,
    CONSTRAINT agents_bootstrap_state_check CHECK ((bootstrap_state = ANY (ARRAY['pending'::text, 'active'::text, 'expired'::text, 'revoked'::text]))),
    CONSTRAINT agents_cert_spki_sha256_check CHECK ((length(cert_spki_sha256) = ANY (ARRAY[0, 32]))),
    CONSTRAINT agents_transport_mode_check CHECK ((transport_mode = ANY (ARRAY['inbound'::text, 'outbound'::text])))
);

CREATE TABLE public.audit_events (
    id text NOT NULL,
    actor_id text NOT NULL,
    action text NOT NULL,
    target_id text NOT NULL,
    details jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone NOT NULL,
    prev_hash text DEFAULT ''::text NOT NULL,
    event_hash text DEFAULT ''::text NOT NULL
);

CREATE TABLE public.certificate_authority (
    scope text NOT NULL,
    ca_pem text NOT NULL,
    private_key_pem text NOT NULL,
    updated_at timestamp with time zone NOT NULL
);

CREATE TABLE public.client_assignments (
    id text NOT NULL,
    client_id text NOT NULL,
    target_type text NOT NULL,
    fleet_group_id uuid,
    agent_id text,
    created_at timestamp with time zone NOT NULL
);

CREATE TABLE public.client_deployments (
    client_id text NOT NULL,
    agent_id text NOT NULL,
    desired_operation text NOT NULL,
    status text NOT NULL,
    last_error text DEFAULT ''::text NOT NULL,
    last_applied_at timestamp with time zone,
    updated_at timestamp with time zone NOT NULL,
    connection_links jsonb DEFAULT '[]'::jsonb NOT NULL,
    last_reset_epoch_secs bigint DEFAULT 0 NOT NULL,
    link_diagnostic text DEFAULT ''::text NOT NULL
);

CREATE TABLE public.client_ip_history (
    agent_id text NOT NULL,
    client_id text NOT NULL,
    ip_address text NOT NULL,
    first_seen timestamp with time zone NOT NULL,
    last_seen timestamp with time zone NOT NULL
);

CREATE TABLE public.client_usage (
    client_id text NOT NULL,
    agent_id text NOT NULL,
    traffic_used_bytes bigint DEFAULT 0 NOT NULL,
    unique_ips_used integer DEFAULT 0 NOT NULL,
    active_tcp_conns integer DEFAULT 0 NOT NULL,
    active_unique_ips integer DEFAULT 0 NOT NULL,
    observed_at timestamp with time zone NOT NULL,
    quota_used_bytes bigint DEFAULT 0 NOT NULL,
    quota_last_reset_unix bigint DEFAULT 0 NOT NULL,
    agent_boot_id text DEFAULT ''::text NOT NULL,
    last_total_bytes bigint DEFAULT 0 NOT NULL
);

CREATE TABLE public.clients (
    id text NOT NULL,
    name text NOT NULL,
    secret_ciphertext text NOT NULL,
    user_ad_tag text NOT NULL,
    enabled boolean DEFAULT true NOT NULL,
    max_tcp_conns bigint DEFAULT 0 NOT NULL,
    max_unique_ips bigint DEFAULT 0 NOT NULL,
    data_quota_bytes bigint DEFAULT 0 NOT NULL,
    expiration_rfc3339 text DEFAULT ''::text NOT NULL,
    created_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone NOT NULL,
    deleted_at timestamp with time zone,
    subscription_token text
);

CREATE TABLE public.config_apply_batch_targets (
    batch_id text NOT NULL,
    agent_id text NOT NULL,
    wave_index integer NOT NULL,
    job_id text DEFAULT ''::text NOT NULL,
    status text NOT NULL,
    message text DEFAULT ''::text NOT NULL,
    CONSTRAINT config_apply_batch_targets_status_check CHECK ((status = ANY (ARRAY['pending'::text, 'running'::text, 'succeeded'::text, 'failed'::text, 'skipped'::text])))
);

CREATE TABLE public.config_apply_batches (
    id text NOT NULL,
    fleet_group_id uuid,
    mode text NOT NULL,
    wave_size integer DEFAULT 1 NOT NULL,
    expected_revision text DEFAULT ''::text NOT NULL,
    status text NOT NULL,
    created_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone NOT NULL,
    CONSTRAINT config_apply_batches_mode_check CHECK ((mode = ANY (ARRAY['all_at_once'::text, 'rolling'::text]))),
    CONSTRAINT config_apply_batches_status_check CHECK ((status = ANY (ARRAY['running'::text, 'succeeded'::text, 'failed'::text, 'halted'::text])))
);

CREATE TABLE public.consumed_totp (
    user_id text NOT NULL,
    code text NOT NULL,
    used_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE public.cp_secrets (
    key text NOT NULL,
    value bytea NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE public.discovered_clients (
    id text NOT NULL,
    agent_id text NOT NULL,
    client_name text NOT NULL,
    secret text DEFAULT ''::text NOT NULL,
    status text DEFAULT 'pending_review'::text NOT NULL,
    total_octets bigint DEFAULT 0 NOT NULL,
    current_connections integer DEFAULT 0 NOT NULL,
    active_unique_ips integer DEFAULT 0 NOT NULL,
    max_tcp_conns integer DEFAULT 0 NOT NULL,
    max_unique_ips integer DEFAULT 0 NOT NULL,
    data_quota_bytes bigint DEFAULT 0 NOT NULL,
    expiration text DEFAULT ''::text NOT NULL,
    discovered_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone NOT NULL,
    connection_links jsonb DEFAULT '[]'::jsonb NOT NULL,
    CONSTRAINT discovered_clients_status_check CHECK ((status = ANY (ARRAY['pending_review'::text, 'adopted'::text, 'ignored'::text])))
);

CREATE TABLE public.enrollment_attempts (
    id uuid NOT NULL,
    token_id uuid,
    agent_id uuid,
    mode text NOT NULL,
    client_addr text,
    request_id text NOT NULL,
    status text NOT NULL,
    error_code text,
    error_message text,
    started_at timestamp with time zone NOT NULL,
    finished_at timestamp with time zone,
    CONSTRAINT enrollment_attempts_mode_check CHECK ((mode = ANY (ARRAY['inbound'::text, 'outbound'::text]))),
    CONSTRAINT enrollment_attempts_status_check CHECK ((status = ANY (ARRAY['in_progress'::text, 'success'::text, 'failed'::text])))
);

CREATE TABLE public.enrollment_events (
    id bigint NOT NULL,
    attempt_id uuid NOT NULL,
    ts timestamp with time zone NOT NULL,
    step text NOT NULL,
    level text NOT NULL,
    message text,
    fields_json jsonb,
    CONSTRAINT enrollment_events_level_check CHECK ((level = ANY (ARRAY['info'::text, 'warn'::text, 'error'::text])))
);

CREATE SEQUENCE public.enrollment_events_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE public.enrollment_events_id_seq OWNED BY public.enrollment_events.id;

CREATE TABLE public.enrollment_tokens (
    value text NOT NULL,
    fleet_group_id uuid,
    issued_at timestamp with time zone NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    consumed_at timestamp with time zone,
    revoked_at timestamp with time zone
);

CREATE TABLE public.fleet_group_integrations (
    id uuid NOT NULL,
    fleet_group_id uuid NOT NULL,
    kind text NOT NULL,
    provider_id uuid,
    config jsonb DEFAULT '{}'::jsonb NOT NULL,
    enabled boolean DEFAULT false NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE public.fleet_groups (
    id uuid NOT NULL,
    name text NOT NULL,
    created_at timestamp with time zone NOT NULL,
    label text DEFAULT ''::text NOT NULL,
    description text DEFAULT ''::text NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE public.integration_providers (
    id uuid NOT NULL,
    kind text NOT NULL,
    label text DEFAULT ''::text NOT NULL,
    config text DEFAULT '{}'::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT integration_providers_config_check CHECK (((config ~~ 'PVS_:%'::text) OR (config IS JSON)))
);

CREATE TABLE public.job_targets (
    job_id text NOT NULL,
    agent_id text NOT NULL,
    status text NOT NULL,
    result_text text DEFAULT ''::text NOT NULL,
    result_json text DEFAULT ''::text NOT NULL,
    updated_at timestamp with time zone NOT NULL,
    CONSTRAINT job_targets_status_check CHECK ((status = ANY (ARRAY['queued'::text, 'sent'::text, 'acknowledged'::text, 'succeeded'::text, 'failed'::text, 'expired'::text])))
);

CREATE TABLE public.jobs (
    id text NOT NULL,
    action text NOT NULL,
    idempotency_key text NOT NULL,
    actor_id text NOT NULL,
    status text NOT NULL,
    created_at timestamp with time zone NOT NULL,
    ttl_nanos bigint NOT NULL,
    payload_json text DEFAULT ''::text NOT NULL,
    CONSTRAINT jobs_status_check CHECK ((status = ANY (ARRAY['queued'::text, 'running'::text, 'succeeded'::text, 'failed'::text, 'expired'::text, 'partial'::text])))
);

CREATE TABLE public.login_lockouts (
    username text NOT NULL,
    failures integer DEFAULT 0 NOT NULL,
    locked_at timestamp with time zone,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE public.metric_snapshots (
    id text NOT NULL,
    agent_id text NOT NULL,
    instance_id text DEFAULT ''::text NOT NULL,
    captured_at timestamp with time zone NOT NULL,
    "values" jsonb NOT NULL
);

CREATE TABLE public.panel_settings (
    scope text NOT NULL,
    http_public_url text DEFAULT ''::text NOT NULL,
    grpc_public_endpoint text DEFAULT ''::text NOT NULL,
    updated_at timestamp with time zone NOT NULL,
    retention_json text DEFAULT ''::text NOT NULL,
    password_min_length integer DEFAULT 10 NOT NULL,
    geoip_json text DEFAULT ''::text NOT NULL,
    geoip_state_json text DEFAULT ''::text NOT NULL,
    CONSTRAINT panel_settings_password_min_length_check CHECK (((password_min_length >= 8) AND (password_min_length <= 128)))
);

CREATE TABLE public.runtime_settings (
    name text NOT NULL,
    value_json text NOT NULL,
    updated_at bigint NOT NULL,
    updated_by text DEFAULT ''::text NOT NULL
);

CREATE TABLE public.sessions (
    id text NOT NULL,
    user_id text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    last_seen_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE public.telemt_diagnostics_current (
    agent_id text NOT NULL,
    observed_at timestamp with time zone NOT NULL,
    state text DEFAULT ''::text NOT NULL,
    state_reason text DEFAULT ''::text NOT NULL,
    system_info_json text DEFAULT '{}'::text NOT NULL,
    effective_limits_json text DEFAULT '{}'::text NOT NULL,
    security_posture_json text DEFAULT '{}'::text NOT NULL,
    minimal_all_json text DEFAULT '{}'::text NOT NULL,
    me_pool_json text DEFAULT '{}'::text NOT NULL,
    dcs_json text DEFAULT '{}'::text NOT NULL
);

CREATE TABLE public.telemt_instances (
    id text NOT NULL,
    agent_id text NOT NULL,
    name text NOT NULL,
    version text DEFAULT ''::text NOT NULL,
    config_fingerprint text DEFAULT ''::text NOT NULL,
    connections bigint DEFAULT 0 CONSTRAINT telemt_instances_connected_users_not_null NOT NULL,
    read_only boolean DEFAULT false NOT NULL,
    updated_at timestamp with time zone NOT NULL
);

CREATE TABLE public.telemt_runtime_current (
    agent_id text NOT NULL,
    observed_at timestamp with time zone NOT NULL,
    runtime_json text DEFAULT ''::text NOT NULL
);

CREATE TABLE public.telemt_runtime_dcs_current (
    agent_id text NOT NULL,
    dc bigint NOT NULL,
    observed_at timestamp with time zone NOT NULL,
    available_endpoints bigint DEFAULT 0 NOT NULL,
    available_pct double precision DEFAULT 0 NOT NULL,
    required_writers bigint DEFAULT 0 NOT NULL,
    alive_writers bigint DEFAULT 0 NOT NULL,
    coverage_pct double precision DEFAULT 0 NOT NULL,
    rtt_ms double precision DEFAULT 0 NOT NULL,
    load double precision DEFAULT 0 NOT NULL
);

CREATE TABLE public.telemt_runtime_events (
    agent_id text NOT NULL,
    sequence bigint NOT NULL,
    observed_at timestamp with time zone NOT NULL,
    timestamp_at timestamp with time zone NOT NULL,
    event_type text DEFAULT ''::text NOT NULL,
    context text DEFAULT ''::text NOT NULL,
    severity text DEFAULT ''::text NOT NULL
);

CREATE TABLE public.telemt_runtime_upstreams_current (
    agent_id text NOT NULL,
    upstream_id bigint NOT NULL,
    observed_at timestamp with time zone NOT NULL,
    route_kind text DEFAULT ''::text NOT NULL,
    address text DEFAULT ''::text NOT NULL,
    healthy boolean DEFAULT false NOT NULL,
    fails bigint DEFAULT 0 NOT NULL,
    effective_latency_ms double precision DEFAULT 0 NOT NULL
);

CREATE TABLE public.telemt_security_inventory_current (
    agent_id text NOT NULL,
    observed_at timestamp with time zone NOT NULL,
    state text DEFAULT ''::text NOT NULL,
    state_reason text DEFAULT ''::text NOT NULL,
    enabled boolean DEFAULT false NOT NULL,
    entries_total bigint DEFAULT 0 NOT NULL,
    entries_json text DEFAULT '[]'::text NOT NULL
);

CREATE TABLE public.ts_dc_health (
    agent_id text NOT NULL,
    captured_at timestamp with time zone NOT NULL,
    dc integer NOT NULL,
    coverage_pct_avg real DEFAULT 0 NOT NULL,
    coverage_pct_min real DEFAULT 0 NOT NULL,
    rtt_ms_avg real DEFAULT 0 NOT NULL,
    rtt_ms_max real DEFAULT 0 NOT NULL,
    alive_writers_min integer DEFAULT 0 NOT NULL,
    required_writers integer DEFAULT 0 NOT NULL,
    load_max integer DEFAULT 0 NOT NULL,
    sample_count integer DEFAULT 1 NOT NULL
);

CREATE TABLE public.ts_server_load (
    agent_id text NOT NULL,
    captured_at timestamp with time zone NOT NULL,
    cpu_pct_avg double precision DEFAULT 0 NOT NULL,
    cpu_pct_max double precision DEFAULT 0 NOT NULL,
    mem_pct_avg double precision DEFAULT 0 NOT NULL,
    mem_pct_max double precision DEFAULT 0 NOT NULL,
    disk_pct_avg double precision DEFAULT 0 NOT NULL,
    disk_pct_max double precision DEFAULT 0 NOT NULL,
    load_1m double precision DEFAULT 0 NOT NULL,
    load_5m double precision DEFAULT 0 NOT NULL,
    load_15m double precision DEFAULT 0 NOT NULL,
    connections_avg integer DEFAULT 0 NOT NULL,
    connections_max integer DEFAULT 0 NOT NULL,
    connections_me_avg integer DEFAULT 0 NOT NULL,
    connections_direct_avg integer DEFAULT 0 NOT NULL,
    active_users_avg integer DEFAULT 0 NOT NULL,
    active_users_max integer DEFAULT 0 NOT NULL,
    connections_total bigint DEFAULT 0 NOT NULL,
    connections_bad_total bigint DEFAULT 0 NOT NULL,
    handshake_timeouts_total bigint DEFAULT 0 NOT NULL,
    dc_coverage_min_pct double precision DEFAULT 0 NOT NULL,
    dc_coverage_avg_pct double precision DEFAULT 0 NOT NULL,
    healthy_upstreams integer DEFAULT 0 NOT NULL,
    total_upstreams integer DEFAULT 0 NOT NULL,
    net_bytes_sent bigint DEFAULT 0 NOT NULL,
    net_bytes_recv bigint DEFAULT 0 NOT NULL,
    sample_count integer DEFAULT 1 NOT NULL
);

CREATE TABLE public.ts_server_load_hourly (
    agent_id text NOT NULL,
    bucket_hour timestamp with time zone NOT NULL,
    cpu_pct_avg real,
    cpu_pct_max real,
    mem_pct_avg real,
    mem_pct_max real,
    connections_avg real,
    connections_max integer,
    active_users_avg real,
    active_users_max integer,
    dc_coverage_min real,
    dc_coverage_avg real,
    sample_count integer DEFAULT 0 NOT NULL
);

CREATE TABLE public.update_config (
    key text NOT NULL,
    value text NOT NULL
);

CREATE TABLE public.user_appearance (
    user_id text NOT NULL,
    theme text DEFAULT 'system'::text NOT NULL,
    density text DEFAULT 'comfortable'::text NOT NULL,
    help_mode text DEFAULT 'basic'::text NOT NULL,
    updated_at timestamp with time zone DEFAULT '1970-01-01 00:00:00+00'::timestamp with time zone NOT NULL
);

CREATE TABLE public.user_fleet_group_scopes (
    user_id text NOT NULL,
    fleet_group_id uuid NOT NULL,
    granted_at timestamp with time zone DEFAULT now() NOT NULL,
    granted_by text DEFAULT ''::text NOT NULL
);

CREATE TABLE public.users (
    id text NOT NULL,
    username text NOT NULL,
    password_hash text NOT NULL,
    role text NOT NULL,
    totp_enabled boolean DEFAULT false NOT NULL,
    totp_secret text DEFAULT ''::text NOT NULL,
    created_at timestamp with time zone NOT NULL
);

CREATE TABLE public.webhook_endpoints (
    id text NOT NULL,
    name text NOT NULL,
    url text NOT NULL,
    secret_ciphertext text NOT NULL,
    event_filter text DEFAULT ''::text NOT NULL,
    allow_private boolean DEFAULT false NOT NULL,
    enabled boolean DEFAULT true NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE public.webhook_outbox (
    id text NOT NULL,
    endpoint_id text NOT NULL,
    event_action text NOT NULL,
    payload jsonb NOT NULL,
    attempt integer DEFAULT 0 NOT NULL,
    next_attempt_at timestamp with time zone NOT NULL,
    last_error text DEFAULT ''::text NOT NULL,
    dead boolean DEFAULT false NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    delivered_at timestamp with time zone
);

ALTER TABLE ONLY public.enrollment_events ALTER COLUMN id SET DEFAULT nextval('public.enrollment_events_id_seq'::regclass);

ALTER TABLE ONLY public.agent_certificate_recovery_grants
    ADD CONSTRAINT agent_certificate_recovery_grants_pkey PRIMARY KEY (agent_id);

ALTER TABLE ONLY public.agent_config_targets
    ADD CONSTRAINT agent_config_targets_pkey PRIMARY KEY (scope_type, scope_id);

ALTER TABLE ONLY public.agent_fallback_state
    ADD CONSTRAINT agent_fallback_state_pkey PRIMARY KEY (agent_id);

ALTER TABLE ONLY public.agent_revocations
    ADD CONSTRAINT agent_revocations_pkey PRIMARY KEY (agent_id);

ALTER TABLE ONLY public.agents
    ADD CONSTRAINT agents_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.audit_events
    ADD CONSTRAINT audit_events_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.certificate_authority
    ADD CONSTRAINT certificate_authority_pkey PRIMARY KEY (scope);

ALTER TABLE ONLY public.client_assignments
    ADD CONSTRAINT client_assignments_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.client_deployments
    ADD CONSTRAINT client_deployments_pkey PRIMARY KEY (client_id, agent_id);

ALTER TABLE ONLY public.client_ip_history
    ADD CONSTRAINT client_ip_history_pkey PRIMARY KEY (agent_id, client_id, ip_address);

ALTER TABLE ONLY public.client_usage
    ADD CONSTRAINT client_usage_pkey PRIMARY KEY (client_id, agent_id);

ALTER TABLE ONLY public.clients
    ADD CONSTRAINT clients_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.config_apply_batch_targets
    ADD CONSTRAINT config_apply_batch_targets_pkey PRIMARY KEY (batch_id, agent_id);

ALTER TABLE ONLY public.config_apply_batches
    ADD CONSTRAINT config_apply_batches_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.consumed_totp
    ADD CONSTRAINT consumed_totp_pkey PRIMARY KEY (user_id, code);

ALTER TABLE ONLY public.cp_secrets
    ADD CONSTRAINT cp_secrets_pkey PRIMARY KEY (key);

ALTER TABLE ONLY public.discovered_clients
    ADD CONSTRAINT discovered_clients_agent_id_client_name_key UNIQUE (agent_id, client_name);

ALTER TABLE ONLY public.discovered_clients
    ADD CONSTRAINT discovered_clients_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.enrollment_attempts
    ADD CONSTRAINT enrollment_attempts_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.enrollment_events
    ADD CONSTRAINT enrollment_events_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.enrollment_tokens
    ADD CONSTRAINT enrollment_tokens_pkey PRIMARY KEY (value);

ALTER TABLE ONLY public.fleet_group_integrations
    ADD CONSTRAINT fleet_group_integrations_fleet_group_id_kind_key UNIQUE (fleet_group_id, kind);

ALTER TABLE ONLY public.fleet_group_integrations
    ADD CONSTRAINT fleet_group_integrations_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.fleet_groups
    ADD CONSTRAINT fleet_groups_name_unique UNIQUE (name);

ALTER TABLE ONLY public.fleet_groups
    ADD CONSTRAINT fleet_groups_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.integration_providers
    ADD CONSTRAINT integration_providers_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.job_targets
    ADD CONSTRAINT job_targets_pkey PRIMARY KEY (job_id, agent_id);

ALTER TABLE ONLY public.jobs
    ADD CONSTRAINT jobs_idempotency_key_key UNIQUE (idempotency_key);

ALTER TABLE ONLY public.jobs
    ADD CONSTRAINT jobs_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.login_lockouts
    ADD CONSTRAINT login_lockouts_pkey PRIMARY KEY (username);

ALTER TABLE ONLY public.metric_snapshots
    ADD CONSTRAINT metric_snapshots_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.panel_settings
    ADD CONSTRAINT panel_settings_pkey PRIMARY KEY (scope);

ALTER TABLE ONLY public.runtime_settings
    ADD CONSTRAINT runtime_settings_pkey PRIMARY KEY (name);

ALTER TABLE ONLY public.sessions
    ADD CONSTRAINT sessions_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.telemt_diagnostics_current
    ADD CONSTRAINT telemt_diagnostics_current_pkey PRIMARY KEY (agent_id);

ALTER TABLE ONLY public.telemt_instances
    ADD CONSTRAINT telemt_instances_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.telemt_runtime_current
    ADD CONSTRAINT telemt_runtime_current_pkey PRIMARY KEY (agent_id);

ALTER TABLE ONLY public.telemt_runtime_dcs_current
    ADD CONSTRAINT telemt_runtime_dcs_current_pkey PRIMARY KEY (agent_id, dc);

ALTER TABLE ONLY public.telemt_runtime_events
    ADD CONSTRAINT telemt_runtime_events_pkey PRIMARY KEY (agent_id, sequence);

ALTER TABLE ONLY public.telemt_runtime_upstreams_current
    ADD CONSTRAINT telemt_runtime_upstreams_current_pkey PRIMARY KEY (agent_id, upstream_id);

ALTER TABLE ONLY public.telemt_security_inventory_current
    ADD CONSTRAINT telemt_security_inventory_current_pkey PRIMARY KEY (agent_id);

ALTER TABLE ONLY public.ts_dc_health
    ADD CONSTRAINT ts_dc_health_pkey PRIMARY KEY (agent_id, dc, captured_at);

ALTER TABLE ONLY public.ts_server_load_hourly
    ADD CONSTRAINT ts_server_load_hourly_pkey PRIMARY KEY (agent_id, bucket_hour);

ALTER TABLE ONLY public.ts_server_load
    ADD CONSTRAINT ts_server_load_pkey PRIMARY KEY (agent_id, captured_at);

ALTER TABLE ONLY public.update_config
    ADD CONSTRAINT update_config_pkey PRIMARY KEY (key);

ALTER TABLE ONLY public.user_appearance
    ADD CONSTRAINT user_appearance_pkey PRIMARY KEY (user_id);

ALTER TABLE ONLY public.user_fleet_group_scopes
    ADD CONSTRAINT user_fleet_group_scopes_pkey PRIMARY KEY (user_id, fleet_group_id);

ALTER TABLE ONLY public.users
    ADD CONSTRAINT users_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.users
    ADD CONSTRAINT users_username_key UNIQUE (username);

ALTER TABLE ONLY public.webhook_endpoints
    ADD CONSTRAINT webhook_endpoints_name_key UNIQUE (name);

ALTER TABLE ONLY public.webhook_endpoints
    ADD CONSTRAINT webhook_endpoints_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.webhook_outbox
    ADD CONSTRAINT webhook_outbox_pkey PRIMARY KEY (id);

CREATE UNIQUE INDEX clients_subscription_token_key ON public.clients USING btree (subscription_token) WHERE (subscription_token IS NOT NULL);

CREATE INDEX idx_agent_fallback_state_entered_at ON public.agent_fallback_state USING btree (entered_at_unix);

CREATE INDEX idx_agent_revocations_cert_expires_at ON public.agent_revocations USING btree (cert_expires_at);

CREATE INDEX idx_agents_cert_spki_sha256 ON public.agents USING btree (cert_spki_sha256) WHERE (length(cert_spki_sha256) > 0);

CREATE INDEX idx_agents_fleet_group_id ON public.agents USING btree (fleet_group_id);

CREATE INDEX idx_agents_last_seen_at ON public.agents USING btree (last_seen_at);

CREATE INDEX idx_agents_transport_mode ON public.agents USING btree (transport_mode);

CREATE INDEX idx_audit_events_chain_walk ON public.audit_events USING btree (created_at, id);

CREATE INDEX idx_audit_events_created_at ON public.audit_events USING btree (created_at);

CREATE INDEX idx_client_assignments_client_id ON public.client_assignments USING btree (client_id);

CREATE INDEX idx_client_deployments_client_id ON public.client_deployments USING btree (client_id);

CREATE INDEX idx_client_ip_client ON public.client_ip_history USING btree (client_id, last_seen DESC);

CREATE INDEX idx_client_ip_client_addr ON public.client_ip_history USING btree (client_id, ip_address);

CREATE INDEX idx_client_ip_last_seen ON public.client_ip_history USING btree (last_seen);

CREATE INDEX idx_client_usage_agent_id ON public.client_usage USING btree (agent_id);

CREATE INDEX idx_config_apply_batch_targets_batch_wave ON public.config_apply_batch_targets USING btree (batch_id, wave_index);

CREATE INDEX idx_config_apply_batches_status ON public.config_apply_batches USING btree (status);

CREATE INDEX idx_consumed_totp_used_at ON public.consumed_totp USING btree (used_at);

CREATE INDEX idx_discovered_clients_agent_id ON public.discovered_clients USING btree (agent_id);

CREATE UNIQUE INDEX idx_discovered_clients_pending_unique ON public.discovered_clients USING btree (agent_id, client_name) WHERE (status = 'pending_review'::text);

CREATE INDEX idx_enrollment_attempts_agent ON public.enrollment_attempts USING btree (agent_id);

CREATE INDEX idx_enrollment_attempts_started ON public.enrollment_attempts USING btree (started_at);

CREATE INDEX idx_enrollment_attempts_token ON public.enrollment_attempts USING btree (token_id);

CREATE INDEX idx_enrollment_events_attempt ON public.enrollment_events USING btree (attempt_id, ts);

CREATE INDEX idx_enrollment_tokens_fleet_group_id ON public.enrollment_tokens USING btree (fleet_group_id);

CREATE INDEX idx_fleet_group_integrations_fleet_group_id ON public.fleet_group_integrations USING btree (fleet_group_id);

CREATE INDEX idx_fleet_group_integrations_kind ON public.fleet_group_integrations USING btree (kind);

CREATE INDEX idx_integration_providers_kind ON public.integration_providers USING btree (kind);

CREATE INDEX idx_job_targets_agent_id ON public.job_targets USING btree (agent_id);

CREATE INDEX idx_jobs_actor_id ON public.jobs USING btree (actor_id);

CREATE INDEX idx_jobs_created_at ON public.jobs USING btree (created_at);

CREATE INDEX idx_jobs_status ON public.jobs USING btree (status);

CREATE INDEX idx_login_lockouts_locked_at ON public.login_lockouts USING btree (locked_at);

CREATE INDEX idx_metric_snapshots_agent_captured ON public.metric_snapshots USING btree (agent_id, captured_at);

CREATE INDEX idx_metric_snapshots_captured_at ON public.metric_snapshots USING btree (captured_at);

CREATE INDEX idx_sessions_created_at ON public.sessions USING btree (created_at);

CREATE INDEX idx_sessions_user_id ON public.sessions USING btree (user_id);

CREATE INDEX idx_telemt_instances_agent_id ON public.telemt_instances USING btree (agent_id);

CREATE INDEX idx_ts_dc_health_time ON public.ts_dc_health USING btree (agent_id, captured_at DESC);

CREATE INDEX idx_ts_server_load_time ON public.ts_server_load USING btree (agent_id, captured_at DESC);

CREATE INDEX idx_user_fleet_group_scopes_fleet_group_id ON public.user_fleet_group_scopes USING btree (fleet_group_id);

CREATE INDEX idx_user_fleet_group_scopes_user_id ON public.user_fleet_group_scopes USING btree (user_id);

CREATE INDEX idx_webhook_outbox_ready ON public.webhook_outbox USING btree (next_attempt_at) WHERE ((dead = false) AND (delivered_at IS NULL));

ALTER TABLE ONLY public.agent_certificate_recovery_grants
    ADD CONSTRAINT agent_certificate_recovery_grants_agent_id_fkey FOREIGN KEY (agent_id) REFERENCES public.agents(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.agent_fallback_state
    ADD CONSTRAINT agent_fallback_state_agent_id_fkey FOREIGN KEY (agent_id) REFERENCES public.agents(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.agents
    ADD CONSTRAINT agents_fleet_group_id_fkey FOREIGN KEY (fleet_group_id) REFERENCES public.fleet_groups(id);

ALTER TABLE ONLY public.client_assignments
    ADD CONSTRAINT client_assignments_client_id_fkey FOREIGN KEY (client_id) REFERENCES public.clients(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.client_deployments
    ADD CONSTRAINT client_deployments_client_id_fkey FOREIGN KEY (client_id) REFERENCES public.clients(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.client_usage
    ADD CONSTRAINT client_usage_agent_id_fkey FOREIGN KEY (agent_id) REFERENCES public.agents(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.client_usage
    ADD CONSTRAINT client_usage_client_id_fkey FOREIGN KEY (client_id) REFERENCES public.clients(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.config_apply_batch_targets
    ADD CONSTRAINT config_apply_batch_targets_batch_id_fkey FOREIGN KEY (batch_id) REFERENCES public.config_apply_batches(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.config_apply_batches
    ADD CONSTRAINT config_apply_batches_fleet_group_id_fkey FOREIGN KEY (fleet_group_id) REFERENCES public.fleet_groups(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.discovered_clients
    ADD CONSTRAINT discovered_clients_agent_id_fkey FOREIGN KEY (agent_id) REFERENCES public.agents(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.enrollment_events
    ADD CONSTRAINT enrollment_events_attempt_id_fkey FOREIGN KEY (attempt_id) REFERENCES public.enrollment_attempts(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.enrollment_tokens
    ADD CONSTRAINT enrollment_tokens_fleet_group_id_fkey FOREIGN KEY (fleet_group_id) REFERENCES public.fleet_groups(id) ON DELETE SET NULL;

ALTER TABLE ONLY public.client_assignments
    ADD CONSTRAINT fk_client_assignments_agent_id FOREIGN KEY (agent_id) REFERENCES public.agents(id) ON DELETE SET NULL;

ALTER TABLE ONLY public.client_assignments
    ADD CONSTRAINT fk_client_assignments_fleet_group_id FOREIGN KEY (fleet_group_id) REFERENCES public.fleet_groups(id) ON DELETE SET NULL;

ALTER TABLE ONLY public.client_deployments
    ADD CONSTRAINT fk_client_deployments_agent_id FOREIGN KEY (agent_id) REFERENCES public.agents(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.metric_snapshots
    ADD CONSTRAINT fk_metric_snapshots_agent_id FOREIGN KEY (agent_id) REFERENCES public.agents(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.sessions
    ADD CONSTRAINT fk_sessions_user_id FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.fleet_group_integrations
    ADD CONSTRAINT fleet_group_integrations_fleet_group_id_fkey FOREIGN KEY (fleet_group_id) REFERENCES public.fleet_groups(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.fleet_group_integrations
    ADD CONSTRAINT fleet_group_integrations_provider_id_fkey FOREIGN KEY (provider_id) REFERENCES public.integration_providers(id) ON DELETE SET NULL;

ALTER TABLE ONLY public.job_targets
    ADD CONSTRAINT job_targets_job_id_fkey FOREIGN KEY (job_id) REFERENCES public.jobs(id);

ALTER TABLE ONLY public.telemt_diagnostics_current
    ADD CONSTRAINT telemt_diagnostics_current_agent_id_fkey FOREIGN KEY (agent_id) REFERENCES public.agents(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.telemt_instances
    ADD CONSTRAINT telemt_instances_agent_id_fkey FOREIGN KEY (agent_id) REFERENCES public.agents(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.telemt_runtime_current
    ADD CONSTRAINT telemt_runtime_current_agent_id_fkey FOREIGN KEY (agent_id) REFERENCES public.agents(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.telemt_runtime_dcs_current
    ADD CONSTRAINT telemt_runtime_dcs_current_agent_id_fkey FOREIGN KEY (agent_id) REFERENCES public.agents(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.telemt_runtime_events
    ADD CONSTRAINT telemt_runtime_events_agent_id_fkey FOREIGN KEY (agent_id) REFERENCES public.agents(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.telemt_runtime_upstreams_current
    ADD CONSTRAINT telemt_runtime_upstreams_current_agent_id_fkey FOREIGN KEY (agent_id) REFERENCES public.agents(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.telemt_security_inventory_current
    ADD CONSTRAINT telemt_security_inventory_current_agent_id_fkey FOREIGN KEY (agent_id) REFERENCES public.agents(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.user_appearance
    ADD CONSTRAINT user_appearance_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.user_fleet_group_scopes
    ADD CONSTRAINT user_fleet_group_scopes_fleet_group_id_fkey FOREIGN KEY (fleet_group_id) REFERENCES public.fleet_groups(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.user_fleet_group_scopes
    ADD CONSTRAINT user_fleet_group_scopes_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.webhook_outbox
    ADD CONSTRAINT webhook_outbox_endpoint_id_fkey FOREIGN KEY (endpoint_id) REFERENCES public.webhook_endpoints(id) ON DELETE CASCADE;


-- +goose Down
-- Squash-init: даунгрейда некуда — no-op.
SELECT 1;
