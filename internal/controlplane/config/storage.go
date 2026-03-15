package config

import (
	"errors"
	"fmt"
	"strings"
)

const (
	// StorageDriverSQLite identifies the default embedded database backend.
	StorageDriverSQLite = "sqlite"
	// StorageDriverPostgres identifies the external PostgreSQL backend.
	StorageDriverPostgres = "postgres"
	// DefaultSQLiteDSN points to the default local database file.
	DefaultSQLiteDSN = "data/panvex.db"
)

var (
	// ErrUnsupportedStorageDriver reports an unknown configured storage backend.
	ErrUnsupportedStorageDriver = errors.New("unsupported storage driver")
	// ErrStorageDSNRequired reports a missing DSN for a backend that has no safe default.
	ErrStorageDSNRequired = errors.New("storage dsn is required")
)

// StorageConfig describes the selected persistent storage backend.
type StorageConfig struct {
	Driver string
	DSN    string
}

// ResolveStorage normalizes storage backend input and applies safe defaults.
func ResolveStorage(driver string, dsn string) (StorageConfig, error) {
	normalizedDriver := strings.ToLower(strings.TrimSpace(driver))
	normalizedDSN := strings.TrimSpace(dsn)

	if normalizedDriver == "" {
		normalizedDriver = StorageDriverSQLite
	}

	switch normalizedDriver {
	case StorageDriverSQLite:
		if normalizedDSN == "" {
			normalizedDSN = DefaultSQLiteDSN
		}
	case StorageDriverPostgres:
		if normalizedDSN == "" {
			return StorageConfig{}, ErrStorageDSNRequired
		}
	default:
		return StorageConfig{}, fmt.Errorf("%w: %s", ErrUnsupportedStorageDriver, normalizedDriver)
	}

	return StorageConfig{
		Driver: normalizedDriver,
		DSN:    normalizedDSN,
	}, nil
}
