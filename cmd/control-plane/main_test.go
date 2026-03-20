package main

import (
	"context"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/panvex/panvex/internal/controlplane/auth"
	"github.com/panvex/panvex/internal/controlplane/config"
	"github.com/panvex/panvex/internal/controlplane/server"
	"github.com/panvex/panvex/internal/controlplane/storage"
	"github.com/panvex/panvex/internal/controlplane/storage/sqlite"
)

func TestParseServeConfigDefaultsToSQLiteDataFile(t *testing.T) {
	configuration, err := parseServeConfig(nil)
	if err != nil {
		t.Fatalf("parseServeConfig() error = %v", err)
	}

	if configuration.Storage.Driver != config.StorageDriverSQLite {
		t.Fatalf("configuration.Storage.Driver = %q, want %q", configuration.Storage.Driver, config.StorageDriverSQLite)
	}

	if configuration.Storage.DSN != "data/panvex.db" {
		t.Fatalf("configuration.Storage.DSN = %q, want %q", configuration.Storage.DSN, "data/panvex.db")
	}
}

func TestParseServeConfigRejectsPostgresWithoutDSN(t *testing.T) {
	if _, err := parseServeConfig([]string{"-storage-driver", "postgres"}); err == nil {
		t.Fatal("parseServeConfig() error = nil, want postgres DSN validation failure")
	}
}

func TestParseServeConfigAcceptsSupervisedRestartMode(t *testing.T) {
	configuration, err := parseServeConfig([]string{"-restart-mode", "supervised"})
	if err != nil {
		t.Fatalf("parseServeConfig() error = %v", err)
	}

	if configuration.RestartMode != "supervised" {
		t.Fatalf("configuration.RestartMode = %q, want %q", configuration.RestartMode, "supervised")
	}
}

func TestParseServeConfigLoadsConfigFileWhenExplicitlyRequested(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(configPath, []byte(`
[storage]
driver = "sqlite"
dsn = "data/runtime.db"

[http]
listen_address = ":19080"
root_path = "/runtime"

[grpc]
listen_address = ":19443"

[panel]
restart_mode = "supervised"
`), 0o600); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}

	configuration, err := parseServeConfig([]string{"-config", configPath})
	if err != nil {
		t.Fatalf("parseServeConfig() error = %v", err)
	}

	if configuration.ConfigPath != configPath {
		t.Fatalf("configuration.ConfigPath = %q, want %q", configuration.ConfigPath, configPath)
	}
	if !configuration.ConfigManagedRuntime {
		t.Fatal("configuration.ConfigManagedRuntime = false, want true")
	}
	expectedDSN := filepath.Join(filepath.Dir(configPath), "data", "runtime.db")
	if configuration.Storage.DSN != expectedDSN {
		t.Fatalf("configuration.Storage.DSN = %q, want %q", configuration.Storage.DSN, expectedDSN)
	}
	if configuration.HTTPAddr != ":19080" {
		t.Fatalf("configuration.HTTPAddr = %q, want %q", configuration.HTTPAddr, ":19080")
	}
	if configuration.GRPCAddr != ":19443" {
		t.Fatalf("configuration.GRPCAddr = %q, want %q", configuration.GRPCAddr, ":19443")
	}
	if configuration.HTTPRootPath != "/runtime" {
		t.Fatalf("configuration.HTTPRootPath = %q, want %q", configuration.HTTPRootPath, "/runtime")
	}
	if configuration.RestartMode != config.RestartModeSupervised {
		t.Fatalf("configuration.RestartMode = %q, want %q", configuration.RestartMode, config.RestartModeSupervised)
	}
}

func TestParseServeConfigRejectsExplicitLegacyRuntimeFlagsWhenConfigFileIsUsed(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(configPath, []byte(`
[storage]
driver = "sqlite"
dsn = "data/runtime.db"
`), 0o600); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}

	_, err := parseServeConfig([]string{"-config", configPath, "-http-addr", ":9999"})
	if err == nil {
		t.Fatal("parseServeConfig() error = nil, want legacy runtime conflict")
	}
}

func TestResolvePanelRuntimeUsesConfigManagedValuesWhenConfigFileIsPresent(t *testing.T) {
	now := time.Date(2026, time.March, 20, 18, 0, 0, 0, time.UTC)
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	if err := store.PutPanelSettings(context.Background(), storage.PanelSettingsRecord{
		HTTPPublicURL:      "https://panel.example.com",
		GRPCPublicEndpoint: "grpc.panel.example.com:443",
		UpdatedAt:          now,
	}); err != nil {
		t.Fatalf("PutPanelSettings() error = %v", err)
	}

	runtime, err := resolvePanelRuntime(serveConfig{
		HTTPAddr:             ":18080",
		HTTPRootPath:         "/from-config",
		GRPCAddr:             ":18443",
		RestartMode:          config.RestartModeSupervised,
		TLSMode:              config.PanelTLSModeProxy,
		ConfigManagedRuntime: true,
		ConfigPath:           "/etc/panvex/config.toml",
	})
	if err != nil {
		t.Fatalf("resolvePanelRuntime() error = %v", err)
	}

	if runtime.HTTPListenAddress != ":18080" {
		t.Fatalf("runtime.HTTPListenAddress = %q, want %q", runtime.HTTPListenAddress, ":18080")
	}
	if runtime.HTTPRootPath != "/from-config" {
		t.Fatalf("runtime.HTTPRootPath = %q, want %q", runtime.HTTPRootPath, "/from-config")
	}
	if runtime.GRPCListenAddress != ":18443" {
		t.Fatalf("runtime.GRPCListenAddress = %q, want %q", runtime.GRPCListenAddress, ":18443")
	}
	if runtime.TLSMode != config.PanelTLSModeProxy {
		t.Fatalf("runtime.TLSMode = %q, want %q", runtime.TLSMode, config.PanelTLSModeProxy)
	}
	if runtime.ConfigPath != "/etc/panvex/config.toml" {
		t.Fatalf("runtime.ConfigPath = %q, want %q", runtime.ConfigPath, "/etc/panvex/config.toml")
	}
	if runtime.ConfigSource != server.PanelRuntimeSourceConfigFile {
		t.Fatalf("runtime.ConfigSource = %q, want %q", runtime.ConfigSource, server.PanelRuntimeSourceConfigFile)
	}
	if !runtime.RestartSupported {
		t.Fatal("runtime.RestartSupported = false, want true")
	}
}

func TestResolvePanelRuntimeIgnoresStoredSharedSettingsWhenUsingLegacyStartup(t *testing.T) {
	now := time.Date(2026, time.March, 16, 22, 10, 0, 0, time.UTC)
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	if err := store.PutPanelSettings(context.Background(), storage.PanelSettingsRecord{
		HTTPPublicURL:      "https://panel.example.com",
		GRPCPublicEndpoint: "grpc.panel.example.com:443",
		UpdatedAt:          now,
	}); err != nil {
		t.Fatalf("PutPanelSettings() error = %v", err)
	}

	runtime, err := resolvePanelRuntime(serveConfig{
		HTTPAddr: ":8080",
		GRPCAddr: ":8443",
	})
	if err != nil {
		t.Fatalf("resolvePanelRuntime() error = %v", err)
	}

	if runtime.HTTPListenAddress != ":8080" {
		t.Fatalf("runtime.HTTPListenAddress = %q, want %q", runtime.HTTPListenAddress, ":8080")
	}
	if runtime.HTTPRootPath != "" {
		t.Fatalf("runtime.HTTPRootPath = %q, want empty", runtime.HTTPRootPath)
	}
	if runtime.GRPCListenAddress != ":8443" {
		t.Fatalf("runtime.GRPCListenAddress = %q, want %q", runtime.GRPCListenAddress, ":8443")
	}
	if runtime.TLSMode != "proxy" {
		t.Fatalf("runtime.TLSMode = %q, want %q", runtime.TLSMode, "proxy")
	}
}

func TestResolvePanelRuntimeFallsBackToServeConfigDefaults(t *testing.T) {
	runtime, err := resolvePanelRuntime(serveConfig{
		HTTPAddr: ":8080",
		GRPCAddr: ":8443",
	})
	if err != nil {
		t.Fatalf("resolvePanelRuntime() error = %v", err)
	}

	if runtime.HTTPListenAddress != ":8080" {
		t.Fatalf("runtime.HTTPListenAddress = %q, want %q", runtime.HTTPListenAddress, ":8080")
	}
	if runtime.HTTPRootPath != "" {
		t.Fatalf("runtime.HTTPRootPath = %q, want empty", runtime.HTTPRootPath)
	}
	if runtime.GRPCListenAddress != ":8443" {
		t.Fatalf("runtime.GRPCListenAddress = %q, want %q", runtime.GRPCListenAddress, ":8443")
	}
	if runtime.TLSMode != "proxy" {
		t.Fatalf("runtime.TLSMode = %q, want %q", runtime.TLSMode, "proxy")
	}
	if runtime.RestartSupported {
		t.Fatal("runtime.RestartSupported = true, want false")
	}
}

func TestResolvePanelRuntimeMarksSupervisedRestartAsSupported(t *testing.T) {
	runtime, err := resolvePanelRuntime(serveConfig{
		HTTPAddr:     ":8080",
		GRPCAddr:     ":8443",
		RestartMode:  "supervised",
	})
	if err != nil {
		t.Fatalf("resolvePanelRuntime() error = %v", err)
	}

	if !runtime.RestartSupported {
		t.Fatal("runtime.RestartSupported = false, want true")
	}
}

func TestRunBootstrapAdminWritesAdminIntoSelectedBackend(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "panvex.db")
	if err := runBootstrapAdmin([]string{
		"-storage-driver", "sqlite",
		"-storage-dsn", databasePath,
		"-username", "admin",
		"-password", "StrongPassword123!",
	}); err != nil {
		t.Fatalf("runBootstrapAdmin() error = %v", err)
	}

	store, err := sqlite.Open(databasePath)
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	user, err := store.GetUserByUsername(context.Background(), "admin")
	if err != nil {
		t.Fatalf("GetUserByUsername() error = %v", err)
	}

	if user.Username != "admin" {
		t.Fatalf("user.Username = %q, want %q", user.Username, "admin")
	}

	if user.TotpEnabled {
		t.Fatal("user.TotpEnabled = true, want false")
	}

	if user.TotpSecret != "" {
		t.Fatalf("user.TotpSecret = %q, want empty", user.TotpSecret)
	}
}

func TestRunBootstrapAdminDoesNotPrintTotpSecret(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "panvex.db")
	output := captureStdout(t, func() {
		if err := runBootstrapAdmin([]string{
			"-storage-driver", "sqlite",
			"-storage-dsn", databasePath,
			"-username", "admin",
			"-password", "StrongPassword123!",
		}); err != nil {
			t.Fatalf("runBootstrapAdmin() error = %v", err)
		}
	})

	if strings.Contains(output, "TOTP secret:") {
		t.Fatalf("runBootstrapAdmin() output = %q, want no seeded TOTP secret", output)
	}

	if strings.Contains(output, "otpauth URL:") {
		t.Fatalf("runBootstrapAdmin() output = %q, want no seeded otpauth URL", output)
	}
}

func TestRunResetUserTotpClearsEnabledState(t *testing.T) {
	now := time.Date(2026, time.March, 15, 15, 0, 0, 0, time.UTC)
	databasePath := filepath.Join(t.TempDir(), "panvex.db")

	store, err := sqlite.Open(databasePath)
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}

	service := auth.NewServiceWithStore(store)
	user, _, err := service.BootstrapUser(auth.BootstrapInput{
		Username: "admin",
		Password: "StrongPassword123!",
		Role:     auth.RoleAdmin,
	}, now)
	if err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}

	secret, err := service.StartTotpSetup(user.ID, now)
	if err != nil {
		t.Fatalf("StartTotpSetup() error = %v", err)
	}

	code, err := service.GenerateTotpCode(secret, now)
	if err != nil {
		t.Fatalf("GenerateTotpCode() error = %v", err)
	}

	if _, err := service.EnableTotp(user.ID, "StrongPassword123!", code, now); err != nil {
		t.Fatalf("EnableTotp() error = %v", err)
	}

	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	if err := runResetUserTotp([]string{
		"-storage-driver", "sqlite",
		"-storage-dsn", databasePath,
		"-username", "admin",
	}); err != nil {
		t.Fatalf("runResetUserTotp() error = %v", err)
	}

	reopened, err := sqlite.Open(databasePath)
	if err != nil {
		t.Fatalf("sqlite.Open() reopen error = %v", err)
	}
	defer reopened.Close()

	record, err := reopened.GetUserByUsername(context.Background(), "admin")
	if err != nil {
		t.Fatalf("GetUserByUsername() error = %v", err)
	}

	if record.TotpEnabled {
		t.Fatal("record.TotpEnabled = true, want false")
	}

	if record.TotpSecret != "" {
		t.Fatalf("record.TotpSecret = %q, want empty", record.TotpSecret)
	}

	events, err := reopened.ListAuditEvents(context.Background())
	if err != nil {
		t.Fatalf("ListAuditEvents() error = %v", err)
	}

	found := false
	for _, event := range events {
		if event.Action == "auth.totp.reset_by_cli" && event.TargetID == user.ID {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("ListAuditEvents() = %+v, want auth.totp.reset_by_cli event", events)
	}
}

func TestRunResetUserTotpRejectsMissingUser(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "panvex.db")
	output := captureStdout(t, func() {
		err := runResetUserTotp([]string{
			"-storage-driver", "sqlite",
			"-storage-dsn", databasePath,
			"-username", "missing",
		})
		if err == nil {
			t.Fatal("runResetUserTotp() error = nil, want missing user failure")
		}
		if !strings.Contains(err.Error(), `user "missing" not found`) {
			t.Fatalf("runResetUserTotp() error = %v, want missing user message", err)
		}
	})

	if output != "" {
		t.Fatalf("runResetUserTotp() output = %q, want empty on failure", output)
	}
}

func TestResolveEmbeddedUIFilesReturnsUIWhenIndexExists(t *testing.T) {
	uiFiles := resolveEmbeddedUIFiles(fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("<html><body>panvex</body></html>")},
		"assets/app.js": &fstest.MapFile{Data: []byte("console.log('panvex')")},
	})
	if uiFiles == nil {
		t.Fatal("resolveEmbeddedUIFiles() = nil, want embedded UI filesystem")
	}

	indexFile, err := fs.ReadFile(uiFiles, "index.html")
	if err != nil {
		t.Fatalf("fs.ReadFile(index.html) error = %v", err)
	}

	if string(indexFile) != "<html><body>panvex</body></html>" {
		t.Fatalf("index.html = %q, want embedded index", string(indexFile))
	}
}

func TestResolveEmbeddedUIFilesReturnsNilWithoutIndex(t *testing.T) {
	uiFiles := resolveEmbeddedUIFiles(fstest.MapFS{
		"placeholder.txt": &fstest.MapFile{Data: []byte("build frontend assets here")},
	})
	if uiFiles != nil {
		t.Fatal("resolveEmbeddedUIFiles() != nil, want nil when index.html is missing")
	}
}

func TestEmbeddedUIFilesReturnsNilInDefaultBuild(t *testing.T) {
	if embeddedUIFiles() != nil {
		t.Fatal("embeddedUIFiles() != nil, want nil without embeddedui build tag")
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	originalStdout := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() error = %v", err)
	}

	os.Stdout = writer
	defer func() {
		os.Stdout = originalStdout
	}()

	fn()

	if err := writer.Close(); err != nil {
		t.Fatalf("writer.Close() error = %v", err)
	}

	output, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("io.ReadAll() error = %v", err)
	}

	if err := reader.Close(); err != nil {
		t.Fatalf("reader.Close() error = %v", err)
	}

	return string(output)
}
