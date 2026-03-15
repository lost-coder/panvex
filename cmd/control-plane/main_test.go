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
