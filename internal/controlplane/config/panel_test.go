package config

import (
	"errors"
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

func TestResolveLegacyControlPlaneConfigRejectsSSLModeDisable(t *testing.T) {
	t.Setenv(EnvAllowInsecureDB, "")
	_, err := ResolveLegacyControlPlaneConfig(
		":8080", ":8443", RestartModeDisabled, PanelTLSModeProxy,
		StorageDriverPostgres, "postgres://panvex:secret@db.internal:5432/panvex?sslmode=disable",
	)
	if !errors.Is(err, ErrInsecureDBDSN) {
		t.Fatalf("error = %v, want ErrInsecureDBDSN", err)
	}
}

func TestResolveLegacyControlPlaneConfigAllowsSSLModeDisableWithOptIn(t *testing.T) {
	t.Setenv(EnvAllowInsecureDB, "1")
	cfg, err := ResolveLegacyControlPlaneConfig(
		":8080", ":8443", RestartModeDisabled, PanelTLSModeProxy,
		StorageDriverPostgres, "postgres://panvex:secret@127.0.0.1:5432/panvex?sslmode=disable",
	)
	if err != nil {
		t.Fatalf("unexpected error with opt-in: %v", err)
	}
	if cfg.Storage.Driver != StorageDriverPostgres {
		t.Fatalf("storage driver = %q", cfg.Storage.Driver)
	}
}

func TestValidateStorageSecurityKeywordForm(t *testing.T) {
	t.Setenv(EnvAllowInsecureDB, "")
	err := ValidateStorageSecurity(StorageConfig{
		Driver: StorageDriverPostgres,
		DSN:    "host=127.0.0.1 user=panvex password=secret dbname=panvex sslmode=disable",
	})
	if !errors.Is(err, ErrInsecureDBDSN) {
		t.Fatalf("keyword-form DSN with sslmode=disable: error = %v, want ErrInsecureDBDSN", err)
	}
}

func TestNormalizeControlPlaneRootPathCleansTraversal(t *testing.T) {
	// path.Clean already eats `..` on absolute inputs; S12 adds an
	// explicit tripwire so regressions fail fast. Each case documents
	// an input operators might type by mistake and the cleaned result.
	cases := []struct {
		in, want string
	}{
		{"", ""},
		{"/", ""},
		{"/panel", "/panel"},
		{"panel", "/panel"},
		{"/panel/", "/panel"},
		{"/panel/..", ""},
		{"/panel/../admin", "/admin"},
		{"/../../etc", "/etc"},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got, err := normalizeControlPlaneRootPath(tc.in)
			if err != nil {
				t.Fatalf("unexpected error for %q: %v", tc.in, err)
			}
			if got != tc.want {
				t.Fatalf("normalizeControlPlaneRootPath(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestValidateStorageSecuritySQLiteIsUnaffected(t *testing.T) {
	t.Setenv(EnvAllowInsecureDB, "")
	if err := ValidateStorageSecurity(StorageConfig{
		Driver: StorageDriverSQLite,
		DSN:    "data/panvex.db",
	}); err != nil {
		t.Fatalf("sqlite DSN should never fail S10: %v", err)
	}
}

func TestApplyDSNPasswordFromEnv(t *testing.T) {
	cases := []struct {
		name     string
		driver   string
		dsn      string
		password string
		want     string
	}{
		{
			name:     "postgres url without password gets env password",
			driver:   StorageDriverPostgres,
			dsn:      "postgres://panvex@db.internal:5432/panvex?sslmode=require",
			password: "env-secret",
			want:     "postgres://panvex:env-secret@db.internal:5432/panvex?sslmode=require",
		},
		{
			name:     "postgres url with placeholder password is overridden",
			driver:   StorageDriverPostgres,
			dsn:      "postgres://panvex:placeholder@db.internal:5432/panvex?sslmode=require",
			password: "env-secret",
			want:     "postgres://panvex:env-secret@db.internal:5432/panvex?sslmode=require",
		},
		{
			name:     "empty env keeps dsn untouched",
			driver:   StorageDriverPostgres,
			dsn:      "postgres://panvex:literal@db.internal:5432/panvex?sslmode=require",
			password: "",
			want:     "postgres://panvex:literal@db.internal:5432/panvex?sslmode=require",
		},
		{
			name:     "keyword form dsn is left as-is",
			driver:   StorageDriverPostgres,
			dsn:      "host=db.internal user=panvex password=literal dbname=panvex sslmode=require",
			password: "env-secret",
			want:     "host=db.internal user=panvex password=literal dbname=panvex sslmode=require",
		},
		{
			name:     "sqlite driver is never rewritten",
			driver:   StorageDriverSQLite,
			dsn:      "file:/var/lib/panvex/panvex.db",
			password: "env-secret",
			want:     "file:/var/lib/panvex/panvex.db",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := applyDSNPasswordFromEnv(tc.driver, tc.dsn, tc.password)
			if got != tc.want {
				t.Fatalf("applyDSNPasswordFromEnv() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestLoadControlPlaneConfigAppliesEnvDBPassword(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.toml")
	contents := `
[storage]
driver = "postgres"
dsn    = "postgres://panvex@db.internal:5432/panvex?sslmode=require"

[http]
listen_address = ":8080"

[grpc]
listen_address = ":8443"

[tls]
mode = "proxy"

[panel]
restart_mode = "disabled"
`
	if err := os.WriteFile(configPath, []byte(contents), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv(EnvDBPassword, "env-secret")

	configuration, err := LoadControlPlaneConfig(configPath)
	if err != nil {
		t.Fatalf("LoadControlPlaneConfig() error = %v", err)
	}
	want := "postgres://panvex:env-secret@db.internal:5432/panvex?sslmode=require"
	if configuration.Storage.DSN != want {
		t.Fatalf("configuration.Storage.DSN = %q, want %q", configuration.Storage.DSN, want)
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

	// S10: plaintext-postgres DSN only loads with the explicit opt-in.
	// The test fixture documents a local dev setup, so it sets the env
	// to mirror what a developer must do in their shell.
	t.Setenv(EnvAllowInsecureDB, "1")

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
