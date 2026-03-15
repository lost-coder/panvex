package main

import (
	"context"
	"io/fs"
	"path/filepath"
	"testing"
	"testing/fstest"

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
