# Panvex Settings Reference

_Auto-generated from `internal/controlplane/settings/registry.go`. Do not edit by hand — run `make gen-settings`._

## Bootstrap settings

Bootstrap settings are read once at process start. Edit them via environment variables or `config.toml`. Changes require a panel restart.

| Name | Type | Default | ENV | TOML | Description |
|---|---|---|---|---|---|
| `http.listen_address` | hostport | `:8080` | `PANVEX_HTTP_ADDR` | `http.listen_address` | HTTP bind address for the control-plane API and dashboard. |
| `http.root_path` | string | _(empty)_ | `PANVEX_HTTP_ROOT_PATH` | `http.root_path` | URL prefix when behind a path-rewriting reverse proxy (empty = none). |
| `http.agent_root_path` | string | _(empty)_ | `PANVEX_HTTP_AGENT_ROOT_PATH` | `http.agent_root_path` | URL prefix for the agent gRPC-gateway when fronted separately. |
| `http.panel_allowed_cidrs` | string | _(empty)_ | `PANVEX_PANEL_ALLOWED_CIDRS` | `http.panel_allowed_cidrs` | Comma-separated CIDRs allowed to reach the panel API (empty = no restriction). |
| `http.trusted_proxy_cidrs` | string | _(empty)_ | `PANVEX_TRUSTED_PROXY_CIDRS` | `http.trusted_proxy_cidrs` | Trusted reverse-proxy CIDRs whose X-Forwarded-For headers are honoured. |
| `grpc.listen_address` | hostport | `:8443` | `PANVEX_GRPC_ADDR` | `grpc.listen_address` | gRPC bind address for the agent gateway. |
| `tls.mode` | enum | `proxy` | `PANVEX_TLS_MODE` | `tls.mode` | TLS termination mode. proxy = terminate at reverse proxy; direct = serve TLS from the panel. |
| `tls.cert_file` | string | _(empty)_ | `PANVEX_TLS_CERT_FILE` | `tls.cert_file` | PEM certificate path when tls.mode=direct. |
| `tls.key_file` | string | _(empty)_ | `PANVEX_TLS_KEY_FILE` | `tls.key_file` | PEM private key path when tls.mode=direct. |
| `panel.restart_mode` | enum | `disabled` | `PANVEX_RESTART_MODE` | `panel.restart_mode` | Self-restart capability. supervised requires a process supervisor. |
| `panel.env` | enum | `development` | `PANVEX_ENV` | `panel.env` | Deployment environment. production tightens defaults (cookies, HSTS, ws origin). |
| `panel.multi_tenant` | bool | `false` | `PANVEX_MULTI_TENANT` | `panel.multi_tenant` | Enable per-fleet-group scoping for non-admin users. |
| `storage.driver` | enum | `sqlite` | `PANVEX_STORAGE_DRIVER` | `storage.driver` | Storage backend driver. Use postgres for production deployments. |
| `storage.dsn` | string | — | `PANVEX_STORAGE_DSN` | `storage.dsn` | Storage data source name. Required. SQLite path or postgres URL. |
| `storage.db_password` | string | _(secret, no default)_ | `PANVEX_DB_PASSWORD` | — | Postgres password override. Env-only — keeps the secret out of config files. |
| `storage.allow_insecure_db` | bool | `false` | `PANVEX_ALLOW_INSECURE_DB` | — | Permit Postgres DSNs with sslmode=disable. Env-only safety guard. |
| `storage.allow_empty_db_password` | bool | `false` | `PANVEX_ALLOW_EMPTY_DB_PASSWORD` | — | Permit empty Postgres password. Env-only safety guard for development only. |
| `auth.encryption_key` | string | _(secret, no default)_ | `PANVEX_ENCRYPTION_KEY` | — | Master at-rest encryption key. Required. No default, no TOML. |
| `auth.force_secure_cookie` | enum | `auto` | `PANVEX_FORCE_SECURE_COOKIE` | — | Override the auto-detected Secure cookie flag. Env-only. |
| `auth.hsts_preload` | bool | `false` | `PANVEX_HSTS_PRELOAD` | — | Emit the preload directive in HSTS headers. Env-only. |
| `observability.log_level` | enum | `info` | `PANVEX_LOG_LEVEL` | `observability.log_level` | Logger verbosity. |
| `observability.log_file` | string | _(empty)_ | `PANVEX_LOG_FILE` | `observability.log_file` | Path to log file. Empty = stderr only. |
| `observability.pprof_addr` | string | _(empty)_ | `PANVEX_PPROF_ADDR` | `observability.pprof_addr` | pprof listener host:port. Empty disables pprof. |
| `observability.metrics_scrape_token` | string | _(secret, no default)_ | `PANVEX_METRICS_SCRAPE_TOKEN` | — | Bearer token required to scrape /metrics. Env-only. |
| `updates.install_script_url` | string | _(empty)_ | `PANVEX_INSTALL_SCRIPT_URL` | `updates.install_script_url` | Override default agent install-script URL emitted by /api/agents/{id}/install-command. |
| `agent.client_data_concurrency` | int | `4` | `PANVEX_AGENT_CLIENT_DATA_CONCURRENCY` | `agent.client_data_concurrency` | Per-agent concurrency for the panel-side client-data fetcher. |

## Operational settings

Operational settings are stored in the database and edited via the panel UI or the `/api/settings/values` endpoint.

| Name | Type | Default | ENV | TOML | Description |
|---|---|---|---|---|---|
| `http.public_url` | string | _(empty)_ | — | — | Externally visible URL of the panel; used in agent install scripts. |
| `grpc.public_endpoint` | string | _(empty)_ | — | — | Externally visible gRPC endpoint for agents to dial. |
| `auth.password_min_length` | int | `10` | — | — | Minimum length for newly created or rotated passwords. |
| `retention` | json | — | — | — | Retention policy: how long to keep audit events, metrics, jobs, presence rows. |
| `geoip` | json | — | — | — | GeoIP data source mode (off/local/url) and database paths. |
| `updates.channel` | enum | `stable` | — | — | Release channel used to discover panel + agent updates. |
| `updates.allow_prerelease` | bool | `false` | — | — | Permit prerelease tags in the chosen channel. |
