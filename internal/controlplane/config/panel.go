package config

const (
	// DefaultHTTPListenAddress points to the default control-plane HTTP bind address.
	DefaultHTTPListenAddress = ":8080"
	// DefaultGRPCListenAddress points to the default control-plane gRPC bind address.
	DefaultGRPCListenAddress = ":8443"
	// PanelTLSModeProxy means the panel expects TLS termination in front of it.
	PanelTLSModeProxy = "proxy"
	// PanelTLSModeDirect means the panel serves TLS itself.
	PanelTLSModeDirect = "direct"
	// RestartModeDisabled keeps panel self-restart disabled.
	RestartModeDisabled = "disabled"
	// RestartModeSupervised enables controlled self-exit for supervised restart.
	RestartModeSupervised = "supervised"
	// EnvDBPassword names the env variable whose value overrides the
	// password embedded in the PostgreSQL storage DSN. Set it to keep
	// the secret out of config.toml (where it would also appear in
	// `ps` output and host-level backups).
	EnvDBPassword = "PANVEX_DB_PASSWORD"
)
