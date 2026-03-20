package config

import (
	"errors"
	"path/filepath"
	"path"
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
)

var (
	// ErrInvalidPanelTLSMode reports an unsupported TLS mode in control-plane runtime config.
	ErrInvalidPanelTLSMode = errors.New("invalid panel tls mode")
	// ErrInvalidRestartMode reports an unsupported restart mode in control-plane runtime config.
	ErrInvalidRestartMode = errors.New("invalid restart mode")
)

// ControlPlaneConfig describes startup-critical control-plane runtime configuration.
type ControlPlaneConfig struct {
	Storage           StorageConfig
	HTTPListenAddress string
	HTTPRootPath      string
	GRPCListenAddress string
	RestartMode       string
	TLSMode           string
	TLSCertFile       string
	TLSKeyFile        string
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
	ListenAddress string `toml:"listen_address"`
	RootPath      string `toml:"root_path"`
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

	configuration, err := normalizeControlPlaneConfig(ControlPlaneConfig{
		Storage:           storage,
		HTTPListenAddress: fileConfig.HTTP.ListenAddress,
		HTTPRootPath:      fileConfig.HTTP.RootPath,
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
	configuration.TLSCertFile = rebaseConfigRelativePath(configDirectory, configuration.TLSCertFile)
	configuration.TLSKeyFile = rebaseConfigRelativePath(configDirectory, configuration.TLSKeyFile)
	return configuration, nil
}

func normalizeControlPlaneConfig(configuration ControlPlaneConfig) (ControlPlaneConfig, error) {
	configuration.HTTPListenAddress = strings.TrimSpace(configuration.HTTPListenAddress)
	configuration.HTTPRootPath = normalizeControlPlaneRootPath(configuration.HTTPRootPath)
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

func normalizeControlPlaneRootPath(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || trimmed == "/" {
		return ""
	}
	if !strings.HasPrefix(trimmed, "/") {
		trimmed = "/" + trimmed
	}
	cleaned := path.Clean(trimmed)
	if cleaned == "." || cleaned == "/" {
		return ""
	}
	return cleaned
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
