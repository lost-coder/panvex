package settings

// Bootstrap is the registry of settings read once at process start
// from environment variables and config.toml.
//
// Field order is the canonical order used by codegen. Group fields by
// namespace prefix to keep generated docs and example.config.toml
// coherent.
type Bootstrap struct {
	HTTPListenAddress    string `setting:"name=http.listen_address, type=hostport, default=:8080, env=PANVEX_HTTP_ADDR, toml=http.listen_address, desc='HTTP bind address for the control-plane API and dashboard.'"`
	HTTPRootPath         string `setting:"name=http.root_path, type=string, default=, env=PANVEX_HTTP_ROOT_PATH, toml=http.root_path, desc='URL prefix when behind a path-rewriting reverse proxy (empty = none).'"`
	HTTPAgentRootPath    string `setting:"name=http.agent_root_path, type=string, default=, env=PANVEX_HTTP_AGENT_ROOT_PATH, toml=http.agent_root_path, desc='URL prefix for the agent gRPC-gateway when fronted separately.'"`
	HTTPPanelAllowedCIDRs string `setting:"name=http.panel_allowed_cidrs, type=string, default=, env=PANVEX_PANEL_ALLOWED_CIDRS, toml=http.panel_allowed_cidrs, desc='Comma-separated CIDRs allowed to reach the panel API (empty = no restriction).'"`
	HTTPTrustedProxyCIDRs string `setting:"name=http.trusted_proxy_cidrs, type=string, default=, env=PANVEX_TRUSTED_PROXY_CIDRS, toml=http.trusted_proxy_cidrs, desc='Trusted reverse-proxy CIDRs whose X-Forwarded-For headers are honoured.'"`

	GRPCListenAddress string `setting:"name=grpc.listen_address, type=hostport, default=:8443, env=PANVEX_GRPC_ADDR, toml=grpc.listen_address, desc='gRPC bind address for the agent gateway.'"`

	TLSMode     string `setting:"name=tls.mode, type=enum, values=proxy|direct, default=proxy, env=PANVEX_TLS_MODE, toml=tls.mode, desc='TLS termination mode. proxy = terminate at reverse proxy; direct = serve TLS from the panel.'"`
	TLSCertFile string `setting:"name=tls.cert_file, type=string, default=, env=PANVEX_TLS_CERT_FILE, toml=tls.cert_file, desc='PEM certificate path when tls.mode=direct.'"`
	TLSKeyFile  string `setting:"name=tls.key_file, type=string, default=, env=PANVEX_TLS_KEY_FILE, toml=tls.key_file, desc='PEM private key path when tls.mode=direct.'"`

	PanelRestartMode  string `setting:"name=panel.restart_mode, type=enum, values=disabled|supervised, default=disabled, env=PANVEX_RESTART_MODE, toml=panel.restart_mode, desc='Self-restart capability. supervised requires a process supervisor.'"`
	PanelEnv          string `setting:"name=panel.env, type=enum, values=development|production, default=development, env=PANVEX_ENV, toml=panel.env, desc='Deployment environment. production tightens defaults (cookies, HSTS, ws origin).'"`
	PanelMultiTenant  string `setting:"name=panel.multi_tenant, type=bool, default=false, env=PANVEX_MULTI_TENANT, toml=panel.multi_tenant, desc='Enable per-fleet-group scoping for non-admin users.'"`

	StorageDriver string `setting:"name=storage.driver, type=enum, values=sqlite|postgres, default=sqlite, env=PANVEX_STORAGE_DRIVER, toml=storage.driver, desc='Storage backend driver. Use postgres for production deployments.'"`
	StorageDSN    string `setting:"name=storage.dsn, type=string, env=PANVEX_STORAGE_DSN, toml=storage.dsn, desc='Storage data source name. Required. SQLite path or postgres URL.'"`

	StorageDBPassword           string `setting:"name=storage.db_password, type=string, secret=true, env=PANVEX_DB_PASSWORD, default=, desc='Postgres password override. Env-only — keeps the secret out of config files.'"`
	StorageAllowInsecureDB      string `setting:"name=storage.allow_insecure_db, type=bool, default=false, env=PANVEX_ALLOW_INSECURE_DB, desc='Permit Postgres DSNs with sslmode=disable. Env-only safety guard.'"`
	StorageAllowEmptyDBPassword string `setting:"name=storage.allow_empty_db_password, type=bool, default=false, env=PANVEX_ALLOW_EMPTY_DB_PASSWORD, desc='Permit empty Postgres password. Env-only safety guard for development only.'"`

	AuthEncryptionKey      string `setting:"name=auth.encryption_key, type=string, secret=true, env=PANVEX_ENCRYPTION_KEY, desc='Master at-rest encryption key. Required. No default, no TOML.'"`
	AuthForceSecureCookie  string `setting:"name=auth.force_secure_cookie, type=enum, values=auto|true|false, default=auto, env=PANVEX_FORCE_SECURE_COOKIE, desc='Override the auto-detected Secure cookie flag. Env-only.'"`
	AuthHSTSPreload        string `setting:"name=auth.hsts_preload, type=bool, default=false, env=PANVEX_HSTS_PRELOAD, desc='Emit the preload directive in HSTS headers. Env-only.'"`

	ObservabilityLogLevel          string `setting:"name=observability.log_level, type=enum, values=debug|info|warn|error, default=info, env=PANVEX_LOG_LEVEL, toml=observability.log_level, desc='Logger verbosity.'"`
	ObservabilityLogFile           string `setting:"name=observability.log_file, type=string, default=, env=PANVEX_LOG_FILE, toml=observability.log_file, desc='Path to log file. Empty = stderr only.'"`
	ObservabilityPprofAddr         string `setting:"name=observability.pprof_addr, type=string, default=, env=PANVEX_PPROF_ADDR, toml=observability.pprof_addr, desc='pprof listener host:port. Empty disables pprof.'"`
	ObservabilityMetricsScrapeToken string `setting:"name=observability.metrics_scrape_token, type=string, secret=true, default=, env=PANVEX_METRICS_SCRAPE_TOKEN, desc='Bearer token required to scrape /metrics. Env-only.'"`

	UpdatesInstallScriptURL string `setting:"name=updates.install_script_url, type=string, default=, env=PANVEX_INSTALL_SCRIPT_URL, toml=updates.install_script_url, desc='Override default agent install-script URL emitted by /api/agents/{id}/install-command.'"`

	AgentClientDataConcurrency int `setting:"name=agent.client_data_concurrency, type=int, min=1, max=32, default=4, env=PANVEX_AGENT_CLIENT_DATA_CONCURRENCY, toml=agent.client_data_concurrency, desc='Per-agent concurrency for the panel-side client-data fetcher.'"`
}

// Operational is the registry of settings persisted in the database
// and editable by panel administrators.
type Operational struct {
	HTTPPublicURL      string `setting:"name=http.public_url, type=string, default=, restart=false, store=panel_settings.http_public_url, desc='Externally visible URL of the panel; used in agent install scripts.'"`
	GRPCPublicEndpoint string `setting:"name=grpc.public_endpoint, type=string, default=, restart=false, store=panel_settings.grpc_public_endpoint, desc='Externally visible gRPC endpoint for agents to dial.'"`

	AuthPasswordMinLength int `setting:"name=auth.password_min_length, type=int, default=10, min=8, max=64, restart=false, store=panel_settings.password_min_length, desc='Minimum length for newly created or rotated passwords.'"`

	Retention string `setting:"name=retention, type=json, restart=false, store=panel_settings.retention_json, desc='Retention policy: how long to keep audit events, metrics, jobs, presence rows.'"`
	GeoIP     string `setting:"name=geoip, type=json, restart=false, store=panel_settings.geoip_json, desc='GeoIP data source mode (off/local/url) and database paths.'"`

	UpdatesChannel         string `setting:"name=updates.channel, type=enum, values=stable|beta, default=stable, restart=false, store=runtime_settings, desc='Release channel used to discover panel + agent updates.'"`
	UpdatesAllowPrerelease string `setting:"name=updates.allow_prerelease, type=bool, default=false, restart=false, store=runtime_settings, desc='Permit prerelease tags in the chosen channel.'"`
}
