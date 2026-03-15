package main

import (
	"context"
	"path/filepath"
	"testing"

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
}
