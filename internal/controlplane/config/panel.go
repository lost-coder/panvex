package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
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

type controlPlaneConfigFile struct {
	Storage controlPlaneStorageSection `toml:"storage"`
	HTTP    controlPlaneHTTPSection    `toml:"http"`
	GRPC    controlPlaneGRPCSection    `toml:"grpc"`
	TLS     controlPlaneTLSSection     `toml:"tls"`
	Panel   controlPlanePanelSection   `toml:"panel"`
}

type controlPlaneStorageSection struct {
	Driver string `toml:"driver"`
	DSN    string `toml:"dsn"`
}

type controlPlaneHTTPSection struct {
	ListenAddress     string   `toml:"listen_address"`
	RootPath          string   `toml:"root_path"`
	AgentRootPath     string   `toml:"agent_root_path"`
	PanelAllowedCIDRs []string `toml:"panel_allowed_cidrs"`
}

type controlPlaneGRPCSection struct {
	ListenAddress string `toml:"listen_address"`
}

type controlPlaneTLSSection struct {
	Mode     string `toml:"mode"`
	CertFile string `toml:"cert_file"`
	KeyFile  string `toml:"key_file"`
}

type controlPlanePanelSection struct {
	RestartMode string `toml:"restart_mode"`
}

// ResolveLegacyControlPlaneConfig normalizes the legacy flag-based runtime input.
func ResolveLegacyControlPlaneConfig(httpAddr string, grpcAddr string, restartMode string, tlsMode string, storageDriver string, storageDSN string) (ControlPlaneConfig, error) {
	storage, err := ResolveStorage(storageDriver, storageDSN)
	if err != nil {
		return ControlPlaneConfig{}, err
	}
	if err := ValidateStorageSecurity(storage); err != nil {
		return ControlPlaneConfig{}, err
	}

	return normalizeControlPlaneConfig(ControlPlaneConfig{
		Storage:           storage,
		HTTPListenAddress: httpAddr,
		HTTPRootPath:      "",
		GRPCListenAddress: grpcAddr,
		RestartMode:       restartMode,
		TLSMode:           tlsMode,
	})
}

// LoadControlPlaneConfig reads and normalizes the control-plane runtime configuration from TOML.
func LoadControlPlaneConfig(configPath string) (ControlPlaneConfig, error) {
	var fileConfig controlPlaneConfigFile
	if _, err := toml.DecodeFile(configPath, &fileConfig); err != nil {
		return ControlPlaneConfig{}, err
	}

	storage, err := ResolveStorage(fileConfig.Storage.Driver, fileConfig.Storage.DSN)
	if err != nil {
		return ControlPlaneConfig{}, err
	}
	if err := ValidateStorageSecurity(storage); err != nil {
		return ControlPlaneConfig{}, err
	}

	configuration, err := normalizeControlPlaneConfig(ControlPlaneConfig{
		Storage:           storage,
		HTTPListenAddress: fileConfig.HTTP.ListenAddress,
		HTTPRootPath:      fileConfig.HTTP.RootPath,
		AgentHTTPRootPath: fileConfig.HTTP.AgentRootPath,
		PanelAllowedCIDRs: fileConfig.HTTP.PanelAllowedCIDRs,
		GRPCListenAddress: fileConfig.GRPC.ListenAddress,
		RestartMode:       fileConfig.Panel.RestartMode,
		TLSMode:           fileConfig.TLS.Mode,
		TLSCertFile:       fileConfig.TLS.CertFile,
		TLSKeyFile:        fileConfig.TLS.KeyFile,
	})
	if err != nil {
		return ControlPlaneConfig{}, err
	}

	configDirectory := filepath.Dir(configPath)
	configuration.Storage.DSN = rebaseConfigRelativeSQLiteDSN(configDirectory, configuration.Storage.Driver, configuration.Storage.DSN)
	configuration.Storage.DSN = applyDSNPasswordFromEnv(configuration.Storage.Driver, configuration.Storage.DSN, os.Getenv(EnvDBPassword))
	configuration.TLSCertFile = rebaseConfigRelativePath(configDirectory, configuration.TLSCertFile)
	configuration.TLSKeyFile = rebaseConfigRelativePath(configDirectory, configuration.TLSKeyFile)
	return configuration, nil
}

// applyDSNPasswordFromEnv injects password into a PostgreSQL URL-form DSN
// when EnvDBPassword is set. Keyword/value DSNs are returned unchanged —
// set the password directly in the env there (`PGPASSWORD`) or keep the
// keyword-form DSN self-contained. Non-postgres drivers are no-ops.
func applyDSNPasswordFromEnv(driver, dsn, password string) string {
	if password == "" || driver != StorageDriverPostgres {
		return dsn
	}
	if !strings.Contains(dsn, "://") {
		return dsn
	}
	parsed, err := url.Parse(dsn)
	if err != nil {
		return dsn
	}
	username := ""
	if parsed.User != nil {
		username = parsed.User.Username()
	}
	parsed.User = url.UserPassword(username, password)
	return parsed.String()
}

func normalizeControlPlaneConfig(configuration ControlPlaneConfig) (ControlPlaneConfig, error) {
	configuration.HTTPListenAddress = strings.TrimSpace(configuration.HTTPListenAddress)
	httpRoot, err := normalizeControlPlaneRootPath(configuration.HTTPRootPath)
	if err != nil {
		return ControlPlaneConfig{}, fmt.Errorf("http root_path: %w", err)
	}
	configuration.HTTPRootPath = httpRoot
	agentRoot, err := normalizeControlPlaneRootPath(configuration.AgentHTTPRootPath)
	if err != nil {
		return ControlPlaneConfig{}, fmt.Errorf("http agent_root_path: %w", err)
	}
	configuration.AgentHTTPRootPath = agentRoot
	configuration.GRPCListenAddress = strings.TrimSpace(configuration.GRPCListenAddress)
	configuration.RestartMode = normalizeRestartMode(configuration.RestartMode)
	configuration.TLSMode = normalizePanelTLSMode(configuration.TLSMode)
	configuration.TLSCertFile = strings.TrimSpace(configuration.TLSCertFile)
	configuration.TLSKeyFile = strings.TrimSpace(configuration.TLSKeyFile)

	if configuration.HTTPListenAddress == "" {
		configuration.HTTPListenAddress = DefaultHTTPListenAddress
	}
	if configuration.GRPCListenAddress == "" {
		configuration.GRPCListenAddress = DefaultGRPCListenAddress
	}
	if configuration.RestartMode != RestartModeDisabled && configuration.RestartMode != RestartModeSupervised {
		return ControlPlaneConfig{}, ErrInvalidRestartMode
	}
	if configuration.TLSMode != PanelTLSModeProxy && configuration.TLSMode != PanelTLSModeDirect {
		return ControlPlaneConfig{}, ErrInvalidPanelTLSMode
	}
	if configuration.TLSMode == PanelTLSModeProxy {
		configuration.TLSCertFile = ""
		configuration.TLSKeyFile = ""
	} else if configuration.TLSCertFile == "" || configuration.TLSKeyFile == "" {
		return ControlPlaneConfig{}, errors.New("tls cert_file and key_file are required when tls.mode is direct")
	}

	return configuration, nil
}

func normalizeRestartMode(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if normalized == "" {
		return RestartModeDisabled
	}
	return normalized
}

func normalizePanelTLSMode(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if normalized == "" {
		return PanelTLSModeProxy
	}
	return normalized
}

func normalizeControlPlaneRootPath(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || trimmed == "/" {
		return "", nil
	}
	if !strings.HasPrefix(trimmed, "/") {
		trimmed = "/" + trimmed
	}
	cleaned := path.Clean(trimmed)
	if cleaned == "." || cleaned == "/" {
		return "", nil
	}
	// S12: belt-and-braces check that path.Clean didn't leave a `..`
	// segment. `path.Clean` on an absolute input removes them, so this
	// is a tripwire for accidental regressions (e.g. someone dropping
	// the forced leading-slash prefix above). Rejecting here means a
	// misconfigured root path fails fast at startup instead of
	// producing half-escaped URLs later.
	if cleaned == ".." ||
		strings.HasPrefix(cleaned, "../") ||
		strings.HasSuffix(cleaned, "/..") ||
		strings.Contains(cleaned, "/../") {
		return "", fmt.Errorf("%w: %q would escape the mount point", ErrInvalidRootPath, value)
	}
	if !strings.HasPrefix(cleaned, "/") {
		return "", fmt.Errorf("%w: %q must be absolute", ErrInvalidRootPath, value)
	}
	return cleaned, nil
}

func rebaseConfigRelativeSQLiteDSN(configDirectory string, driver string, dsn string) string {
	if driver != StorageDriverSQLite {
		return dsn
	}
	if strings.TrimSpace(dsn) == "" || dsn == ":memory:" || strings.HasPrefix(dsn, "file:") || strings.Contains(dsn, "://") || isConfigAbsolutePath(dsn) {
		return dsn
	}
	return filepath.Join(configDirectory, dsn)
}

func rebaseConfigRelativePath(configDirectory string, value string) string {
	if strings.TrimSpace(value) == "" || isConfigAbsolutePath(value) {
		return value
	}
	return filepath.Join(configDirectory, value)
}

func isConfigAbsolutePath(value string) bool {
	trimmed := strings.TrimSpace(value)
	return filepath.IsAbs(trimmed) || strings.HasPrefix(trimmed, "/") || strings.HasPrefix(trimmed, "\\")
}
