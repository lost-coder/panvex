package config

import (
	"errors"
)

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

var (
	// ErrInvalidPanelTLSMode reports an unsupported TLS mode in control-plane runtime config.
	ErrInvalidPanelTLSMode = errors.New("invalid panel tls mode")
	// ErrInvalidRestartMode reports an unsupported restart mode in control-plane runtime config.
	ErrInvalidRestartMode = errors.New("invalid restart mode")
	// ErrInvalidRootPath reports a root-path that escapes the public mount
	// point after path cleaning (S12). In practice `path.Clean` eliminates
	// `..` segments from any absolute input, so this check is a tripwire
	// against future refactors that remove the forced leading slash.
	ErrInvalidRootPath = errors.New("invalid root path")
)

// ControlPlaneConfig describes startup-critical control-plane runtime configuration.
type ControlPlaneConfig struct {
	Storage             StorageConfig
	HTTPListenAddress   string
	HTTPRootPath        string
	AgentHTTPRootPath   string
	PanelAllowedCIDRs   []string
	GRPCListenAddress   string
	RestartMode         string
	TLSMode             string
	TLSCertFile         string
	TLSKeyFile          string
}
