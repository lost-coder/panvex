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

	"github.com/lost-coder/panvex/internal/controlplane/auth"
	"github.com/lost-coder/panvex/internal/controlplane/config"
	"github.com/lost-coder/panvex/internal/controlplane/server"
	"github.com/lost-coder/panvex/internal/controlplane/storage"
	"github.com/lost-coder/panvex/internal/controlplane/storage/sqlite"
)

func TestParseServeConfigDefaultsToSQLiteDataFile(t *testing.T) {
	// StorageDSN and AuthEncryptionKey are required by settings.LoadBootstrap
	// and have no registry default — supply them via env.
	t.Setenv("PANVEX_STORAGE_DSN", "data/panvex.db")
	t.Setenv("PANVEX_ENCRYPTION_KEY", "testkey1234567890")
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
	// Set driver to postgres via env; omit DSN — LoadBootstrap should fail.
	t.Setenv("PANVEX_STORAGE_DRIVER", "postgres")
	t.Setenv("PANVEX_ENCRYPTION_KEY", "testkey1234567890")
	if _, err := parseServeConfig(nil); err == nil {
		t.Fatal("parseServeConfig() error = nil, want postgres DSN validation failure")
	}
}

func TestParseServeConfigAcceptsSupervisedRestartMode(t *testing.T) {
	// RestartMode is now read from PANVEX_RESTART_MODE env (or config.toml).
	t.Setenv("PANVEX_RESTART_MODE", "supervised")
	t.Setenv("PANVEX_STORAGE_DSN", "data/panvex.db")
	t.Setenv("PANVEX_ENCRYPTION_KEY", "testkey1234567890")
	configuration, err := parseServeConfig(nil)
	if err != nil {
		t.Fatalf("parseServeConfig() error = %v", err)
	}

	if configuration.RestartMode != "supervised" {
		t.Fatalf("configuration.RestartMode = %q, want %q", configuration.RestartMode, "supervised")
	}
}

func TestParseServeConfigLoadsConfigFileWhenExplicitlyRequested(t *testing.T) {
	// AuthEncryptionKey is required; supply it via env (no TOML binding for secrets).
	t.Setenv("PANVEX_ENCRYPTION_KEY", "testkey1234567890")
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
	// settings.LoadBootstrap reads dsn from TOML and passes it through
	// config.ResolveStorage which rebases relative SQLite paths against
	// the config file directory.
	if configuration.Storage.Driver != config.StorageDriverSQLite {
		t.Fatalf("configuration.Storage.Driver = %q, want %q", configuration.Storage.Driver, config.StorageDriverSQLite)
	}
	// Plan 6: the listen addresses are now DB-backed operational settings,
	// seeded into the store inside server.New — they are no longer carried on
	// serveConfig. The TOML [http]/[grpc] listen_address keys are seed sources
	// read by SeedDefaults, not parseServeConfig.
	if configuration.HTTPRootPath != "/runtime" {
		t.Fatalf("configuration.HTTPRootPath = %q, want %q", configuration.HTTPRootPath, "/runtime")
	}
	if configuration.RestartMode != config.RestartModeSupervised {
		t.Fatalf("configuration.RestartMode = %q, want %q", configuration.RestartMode, config.RestartModeSupervised)
	}
}

func TestParseServeConfigLoadsConfigFileAndEnvTogether(t *testing.T) {
	// The legacy -http-addr / -grpc-addr / -restart-mode / -storage-driver /
	// -storage-dsn flags are removed. Runtime is now controlled entirely via
	// PANVEX_* env vars and config.toml. Verify that -config + env coexist.
	t.Setenv("PANVEX_ENCRYPTION_KEY", "testkey1234567890")
	configPath := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(configPath, []byte(`
[storage]
driver = "sqlite"
dsn = "data/runtime.db"
`), 0o600); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}

	configuration, err := parseServeConfig([]string{"-config", configPath})
	if err != nil {
		t.Fatalf("parseServeConfig() error = %v", err)
	}
	if !configuration.ConfigManagedRuntime {
		t.Fatal("configuration.ConfigManagedRuntime = false, want true")
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
		PasswordMinLength:  10,
		UpdatedAt:          now,
	}); err != nil {
		t.Fatalf("PutPanelSettings() error = %v", err)
	}

	runtime, err := resolvePanelRuntime(serveConfig{
		HTTPRootPath:         "/from-config",
		RestartMode:          config.RestartModeSupervised,
		TLSMode:              config.PanelTLSModeProxy,
		ConfigManagedRuntime: true,
		ConfigPath:           "/etc/panvex/config.toml",
	})
	if err != nil {
		t.Fatalf("resolvePanelRuntime() error = %v", err)
	}

	if runtime.HTTPRootPath != "/from-config" {
		t.Fatalf("runtime.HTTPRootPath = %q, want %q", runtime.HTTPRootPath, "/from-config")
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
		PasswordMinLength:  10,
		UpdatedAt:          now,
	}); err != nil {
		t.Fatalf("PutPanelSettings() error = %v", err)
	}

	runtime, err := resolvePanelRuntime(serveConfig{})
	if err != nil {
		t.Fatalf("resolvePanelRuntime() error = %v", err)
	}

	// Plan 6: listen addresses are no longer carried on PanelRuntime; they
	// are resolved from the store via (*Server).Effective*ListenAddress().
	if runtime.HTTPRootPath != "" {
		t.Fatalf("runtime.HTTPRootPath = %q, want empty", runtime.HTTPRootPath)
	}
	if runtime.TLSMode != "proxy" {
		t.Fatalf("runtime.TLSMode = %q, want %q", runtime.TLSMode, "proxy")
	}
}

func TestResolvePanelRuntimeFallsBackToServeConfigDefaults(t *testing.T) {
	runtime, err := resolvePanelRuntime(serveConfig{})
	if err != nil {
		t.Fatalf("resolvePanelRuntime() error = %v", err)
	}

	if runtime.HTTPRootPath != "" {
		t.Fatalf("runtime.HTTPRootPath = %q, want empty", runtime.HTTPRootPath)
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
		RestartMode: "supervised",
	})
	if err != nil {
		t.Fatalf("resolvePanelRuntime() error = %v", err)
	}

	if !runtime.RestartSupported {
		t.Fatal("runtime.RestartSupported = false, want true")
	}
}

func TestRunBootstrapAdminWritesAdminIntoSelectedBackend(t *testing.T) {
	t.Setenv("PANVEX_BOOTSTRAP_ALLOW_INSECURE_FLAG", "1")
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
	t.Setenv("PANVEX_BOOTSTRAP_ALLOW_INSECURE_FLAG", "1")
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
	user, _, err := service.BootstrapUser(context.Background(), auth.BootstrapInput{
		Username: "admin",
		Password: "StrongPassword123!",
		Role:     auth.RoleAdmin,
	}, now)
	if err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}

	secret, err := service.StartTotpSetup(context.Background(), user.ID, now)
	if err != nil {
		t.Fatalf("StartTotpSetup() error = %v", err)
	}

	code, err := service.GenerateTotpCode(secret, now)
	if err != nil {
		t.Fatalf("GenerateTotpCode() error = %v", err)
	}

	if _, err := service.EnableTotp(context.Background(), user.ID, "StrongPassword123!", code, now); err != nil {
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

	events, err := reopened.ListAuditEvents(context.Background(), 0)
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
		"index.html":    &fstest.MapFile{Data: []byte("<html><body>panvex</body></html>")},
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

func TestResolveEncryptionKeyFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "key.txt")
	if err := os.WriteFile(path, []byte("secret-passphrase\n"), 0600); err != nil {
		t.Fatalf("WriteFile error = %v", err)
	}
	got, err := resolveEncryptionKey(path, false)
	if err != nil {
		t.Fatalf("resolveEncryptionKey error = %v", err)
	}
	if got != "secret-passphrase" {
		t.Fatalf("got %q, want %q", got, "secret-passphrase")
	}
}

func TestResolveEncryptionKeyFallsBackToEnv(t *testing.T) {
	t.Setenv("PANVEX_ENCRYPTION_KEY", "env-passphrase")
	got, err := resolveEncryptionKey("", false)
	if err != nil {
		t.Fatalf("resolveEncryptionKey error = %v", err)
	}
	if got != "env-passphrase" {
		t.Fatalf("got %q, want %q", got, "env-passphrase")
	}
}

func TestResolveEncryptionKeyEmptyWhenUnset(t *testing.T) {
	t.Setenv("PANVEX_ENCRYPTION_KEY", "")
	got, err := resolveEncryptionKey("", false)
	if err != nil {
		t.Fatalf("resolveEncryptionKey error = %v", err)
	}
	if got != "" {
		t.Fatalf("got %q, want empty", got)
	}
}

func TestResolveEncryptionKeyFilePrecedesEnv(t *testing.T) {
	t.Setenv("PANVEX_ENCRYPTION_KEY", "env-passphrase")
	dir := t.TempDir()
	path := filepath.Join(dir, "key.txt")
	if err := os.WriteFile(path, []byte("file-passphrase"), 0600); err != nil {
		t.Fatalf("WriteFile error = %v", err)
	}
	got, err := resolveEncryptionKey(path, false)
	if err != nil {
		t.Fatalf("resolveEncryptionKey error = %v", err)
	}
	if got != "file-passphrase" {
		t.Fatalf("got %q, want %q — file must override env", got, "file-passphrase")
	}
}

func TestResolveEncryptionKeyFileMissing(t *testing.T) {
	if _, err := resolveEncryptionKey("/nonexistent/path/to/key.txt", false); err == nil {
		t.Fatal("resolveEncryptionKey error = nil, want failure for missing file")
	}
}

// TestNewControlPlaneHTTPServerHasHardenedTimeouts guards the P2-REL-02
// regression: every HTTP timeout must be non-zero so a slow client cannot
// stall a goroutine indefinitely. The WebSocket route at /api/events is
// unaffected because coder/websocket hijacks the underlying connection before
// WriteTimeout fires on streaming conns.
func TestNewControlPlaneHTTPServerHasHardenedTimeouts(t *testing.T) {
	httpServer := newControlPlaneHTTPServer(":0", nil)

	if httpServer.ReadHeaderTimeout != httpReadHeaderTimeout {
		t.Fatalf("ReadHeaderTimeout = %v, want %v", httpServer.ReadHeaderTimeout, httpReadHeaderTimeout)
	}
	if httpServer.ReadTimeout != httpReadTimeout {
		t.Fatalf("ReadTimeout = %v, want %v", httpServer.ReadTimeout, httpReadTimeout)
	}
	if httpServer.WriteTimeout != httpWriteTimeout {
		t.Fatalf("WriteTimeout = %v, want %v", httpServer.WriteTimeout, httpWriteTimeout)
	}
	if httpServer.IdleTimeout != httpIdleTimeout {
		t.Fatalf("IdleTimeout = %v, want %v", httpServer.IdleTimeout, httpIdleTimeout)
	}
	if httpServer.ReadTimeout <= 0 || httpServer.WriteTimeout <= 0 {
		t.Fatal("ReadTimeout and WriteTimeout must be positive to prevent slow-client DoS")
	}
}

// TestNewControlPlaneGRPCServerConstructs verifies the P2-REL-01 constructor
// returns a usable server; the keepalive and size options are opaque inside
// grpc.Server, so we assert construction succeeds rather than introspecting
// internal state.
func TestNewControlPlaneGRPCServerConstructs(t *testing.T) {
	grpcServer := newControlPlaneGRPCServer(nil)
	if grpcServer == nil {
		t.Fatal("newControlPlaneGRPCServer() = nil, want *grpc.Server")
	}
	grpcServer.Stop()
}

// TestRunBootstrapAdminUsesPasswordFile verifies that -password-file reads the
// admin password from a file and creates the account successfully (S-10).
func TestRunBootstrapAdminUsesPasswordFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	pwFile := filepath.Join(dir, "admin.pw")
	if err := os.WriteFile(pwFile, []byte("S3curePassword!\n"), 0600); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(dir, "panvex.db")
	err := runBootstrapAdmin([]string{
		"-username", "admin",
		"-password-file", pwFile,
		"-storage-driver", "sqlite",
		"-storage-dsn", dbPath,
	})
	if err != nil {
		t.Fatalf("runBootstrapAdmin: %v", err)
	}

	store, err := sqlite.Open(dbPath)
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
}

// TestRunBootstrapAdminPasswordFileTakesPrecedence verifies that -password-file
// overrides -password when both are supplied (S-10).
func TestRunBootstrapAdminPasswordFileTakesPrecedence(t *testing.T) {
	t.Setenv("PANVEX_BOOTSTRAP_ALLOW_INSECURE_FLAG", "1")
	dir := t.TempDir()
	pwFile := filepath.Join(dir, "admin.pw")
	if err := os.WriteFile(pwFile, []byte("FromFile1234"), 0600); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(dir, "panvex.db")
	// Both flags supplied — file should win.
	err := runBootstrapAdmin([]string{
		"-username", "admin",
		"-password", "FromFlag",
		"-password-file", pwFile,
		"-storage-driver", "sqlite",
		"-storage-dsn", dbPath,
	})
	if err != nil {
		t.Fatalf("runBootstrapAdmin: %v", err)
	}

	store, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	svc := auth.NewServiceWithStore(store)
	// Authenticate with the file password to confirm it was used.
	if _, err := svc.Authenticate(context.Background(), auth.LoginInput{
		Username: "admin",
		Password: "FromFile1234",
	}, time.Now()); err != nil {
		t.Fatalf("Authenticate with file password failed: %v — file password was not used", err)
	}
}
