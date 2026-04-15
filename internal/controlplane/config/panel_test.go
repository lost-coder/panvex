package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveLegacyControlPlaneConfigAppliesDefaults(t *testing.T) {
	configuration, err := ResolveLegacyControlPlaneConfig("", "", "", "", "", "")
	if err != nil {
		t.Fatalf("ResolveLegacyControlPlaneConfig() error = %v", err)
	}

	if configuration.Storage.Driver != StorageDriverSQLite {
		t.Fatalf("configuration.Storage.Driver = %q, want %q", configuration.Storage.Driver, StorageDriverSQLite)
	}
	if configuration.Storage.DSN != DefaultSQLiteDSN {
		t.Fatalf("configuration.Storage.DSN = %q, want %q", configuration.Storage.DSN, DefaultSQLiteDSN)
	}
	if configuration.HTTPListenAddress != ":8080" {
		t.Fatalf("configuration.HTTPListenAddress = %q, want %q", configuration.HTTPListenAddress, ":8080")
	}
	if configuration.GRPCListenAddress != ":8443" {
		t.Fatalf("configuration.GRPCListenAddress = %q, want %q", configuration.GRPCListenAddress, ":8443")
	}
	if configuration.RestartMode != RestartModeDisabled {
		t.Fatalf("configuration.RestartMode = %q, want %q", configuration.RestartMode, RestartModeDisabled)
	}
	if configuration.TLSMode != PanelTLSModeProxy {
		t.Fatalf("configuration.TLSMode = %q, want %q", configuration.TLSMode, PanelTLSModeProxy)
	}
}

func TestResolveLegacyControlPlaneConfigRejectsInvalidTLSMode(t *testing.T) {
	_, err := ResolveLegacyControlPlaneConfig(":8080", ":8443", RestartModeDisabled, "terminating-proxy", "", "")
	if err == nil {
		t.Fatal("ResolveLegacyControlPlaneConfig() error = nil, want invalid tls mode failure")
	}
}

func TestLoadControlPlaneConfigReadsTOMLFile(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(configPath, []byte(`
[storage]
driver = "postgres"
dsn = "postgres://panvex:secret@127.0.0.1:5432/panvex?sslmode=disable"

[http]
listen_address = ":18080"
root_path = "/panel"

[grpc]
listen_address = ":18443"

[tls]
mode = "direct"
cert_file = "/etc/panvex/tls/panel.crt"
key_file = "/etc/panvex/tls/panel.key"

[panel]
restart_mode = "supervised"
`), 0o600); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}

	configuration, err := LoadControlPlaneConfig(configPath)
	if err != nil {
		t.Fatalf("LoadControlPlaneConfig() error = %v", err)
	}

	if configuration.Storage.Driver != StorageDriverPostgres {
		t.Fatalf("configuration.Storage.Driver = %q, want %q", configuration.Storage.Driver, StorageDriverPostgres)
	}
	if configuration.Storage.DSN != "postgres://panvex:secret@127.0.0.1:5432/panvex?sslmode=disable" {
		t.Fatalf("configuration.Storage.DSN = %q, want explicit postgres dsn", configuration.Storage.DSN)
	}
	if configuration.HTTPListenAddress != ":18080" {
		t.Fatalf("configuration.HTTPListenAddress = %q, want %q", configuration.HTTPListenAddress, ":18080")
	}
	if configuration.HTTPRootPath != "/panel" {
		t.Fatalf("configuration.HTTPRootPath = %q, want %q", configuration.HTTPRootPath, "/panel")
	}
	if configuration.GRPCListenAddress != ":18443" {
		t.Fatalf("configuration.GRPCListenAddress = %q, want %q", configuration.GRPCListenAddress, ":18443")
	}
	if configuration.RestartMode != RestartModeSupervised {
		t.Fatalf("configuration.RestartMode = %q, want %q", configuration.RestartMode, RestartModeSupervised)
	}
	if configuration.TLSMode != PanelTLSModeDirect {
		t.Fatalf("configuration.TLSMode = %q, want %q", configuration.TLSMode, PanelTLSModeDirect)
	}
	if configuration.TLSCertFile != "/etc/panvex/tls/panel.crt" {
		t.Fatalf("configuration.TLSCertFile = %q, want %q", configuration.TLSCertFile, "/etc/panvex/tls/panel.crt")
	}
	if configuration.TLSKeyFile != "/etc/panvex/tls/panel.key" {
		t.Fatalf("configuration.TLSKeyFile = %q, want %q", configuration.TLSKeyFile, "/etc/panvex/tls/panel.key")
	}
}

func TestLoadControlPlaneConfigParsesAgentRootPathAndAllowedCIDRs(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(configPath, []byte(`
[storage]
driver = "sqlite"
dsn = "panvex.db"

[http]
listen_address = ":8888"
root_path = "/panel"
agent_root_path = "/agent-api"
panel_allowed_cidrs = ["10.0.0.0/8", "192.168.1.0/24"]

[grpc]
listen_address = ":8443"

[tls]
mode = "proxy"

[panel]
restart_mode = "disabled"
`), 0600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cfg, err := LoadControlPlaneConfig(configPath)
	if err != nil {
		t.Fatalf("LoadControlPlaneConfig() error = %v", err)
	}
	if cfg.AgentHTTPRootPath != "/agent-api" {
		t.Fatalf("AgentHTTPRootPath = %q, want %q", cfg.AgentHTTPRootPath, "/agent-api")
	}
	if len(cfg.PanelAllowedCIDRs) != 2 {
		t.Fatalf("len(PanelAllowedCIDRs) = %d, want 2", len(cfg.PanelAllowedCIDRs))
	}
	if cfg.PanelAllowedCIDRs[0] != "10.0.0.0/8" {
		t.Fatalf("PanelAllowedCIDRs[0] = %q, want %q", cfg.PanelAllowedCIDRs[0], "10.0.0.0/8")
	}
}

func TestLoadControlPlaneConfigEmptyAgentPathAndCIDRs(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(configPath, []byte(`
[storage]
driver = "sqlite"
dsn = "panvex.db"

[http]
listen_address = ":8080"

[grpc]
listen_address = ":8443"

[tls]
mode = "proxy"

[panel]
restart_mode = "disabled"
`), 0600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cfg, err := LoadControlPlaneConfig(configPath)
	if err != nil {
		t.Fatalf("LoadControlPlaneConfig() error = %v", err)
	}
	if cfg.AgentHTTPRootPath != "" {
		t.Fatalf("AgentHTTPRootPath = %q, want empty", cfg.AgentHTTPRootPath)
	}
	if len(cfg.PanelAllowedCIDRs) != 0 {
		t.Fatalf("len(PanelAllowedCIDRs) = %d, want 0", len(cfg.PanelAllowedCIDRs))
	}
}

func TestLoadControlPlaneConfigRebasesRelativePathsToConfigDirectory(t *testing.T) {
	configDir := filepath.Join(t.TempDir(), "config")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("os.MkdirAll() error = %v", err)
	}

	configPath := filepath.Join(configDir, "config.toml")
	if err := os.WriteFile(configPath, []byte(`
[storage]
driver = "sqlite"
dsn = "data/panvex.db"

[tls]
mode = "direct"
cert_file = "tls/panel.crt"
key_file = "tls/panel.key"
`), 0o600); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}

	configuration, err := LoadControlPlaneConfig(configPath)
	if err != nil {
		t.Fatalf("LoadControlPlaneConfig() error = %v", err)
	}

	if configuration.Storage.DSN != filepath.Join(configDir, "data", "panvex.db") {
		t.Fatalf("configuration.Storage.DSN = %q, want rebased config-relative sqlite path", configuration.Storage.DSN)
	}
	if configuration.TLSCertFile != filepath.Join(configDir, "tls", "panel.crt") {
		t.Fatalf("configuration.TLSCertFile = %q, want rebased config-relative cert path", configuration.TLSCertFile)
	}
	if configuration.TLSKeyFile != filepath.Join(configDir, "tls", "panel.key") {
		t.Fatalf("configuration.TLSKeyFile = %q, want rebased config-relative key path", configuration.TLSKeyFile)
	}
}
