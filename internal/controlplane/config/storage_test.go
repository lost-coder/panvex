package config

import (
	"errors"
	"testing"
)

func TestStorageResolveDefaultsToSQLite(t *testing.T) {
	storage, err := ResolveStorage("", "")
	if err != nil {
		t.Fatalf("ResolveStorage() error = %v", err)
	}

	if storage.Driver != StorageDriverSQLite {
		t.Fatalf("storage.Driver = %q, want %q", storage.Driver, StorageDriverSQLite)
	}

	if storage.DSN != DefaultSQLiteDSN {
		t.Fatalf("storage.DSN = %q, want %q", storage.DSN, DefaultSQLiteDSN)
	}
}

func TestStorageResolveAcceptsExplicitPostgres(t *testing.T) {
	storage, err := ResolveStorage("postgres", "postgres://panvex:secret@localhost:5432/panvex?sslmode=disable")
	if err != nil {
		t.Fatalf("ResolveStorage() error = %v", err)
	}

	if storage.Driver != StorageDriverPostgres {
		t.Fatalf("storage.Driver = %q, want %q", storage.Driver, StorageDriverPostgres)
	}

	if storage.DSN != "postgres://panvex:secret@localhost:5432/panvex?sslmode=disable" {
		t.Fatalf("storage.DSN = %q, want explicit postgres DSN", storage.DSN)
	}
}

func TestStorageResolveRejectsUnsupportedDriver(t *testing.T) {
	_, err := ResolveStorage("mysql", "panvex")
	if !errors.Is(err, ErrUnsupportedStorageDriver) {
		t.Fatalf("ResolveStorage() error = %v, want %v", err, ErrUnsupportedStorageDriver)
	}
}

func TestStorageResolveRejectsEmptyPostgresDSN(t *testing.T) {
	_, err := ResolveStorage("postgres", "")
	if !errors.Is(err, ErrStorageDSNRequired) {
		t.Fatalf("ResolveStorage() error = %v, want %v", err, ErrStorageDSNRequired)
	}
}
