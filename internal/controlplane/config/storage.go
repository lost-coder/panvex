package config

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

const (
	// StorageDriverSQLite identifies the default embedded database backend.
	StorageDriverSQLite = "sqlite"
	// StorageDriverPostgres identifies the external PostgreSQL backend.
	StorageDriverPostgres = "postgres"
	// DefaultSQLiteDSN points to the default local database file.
	DefaultSQLiteDSN = "data/panvex.db"
	// EnvAllowInsecureDB opts into `sslmode=disable` on the PostgreSQL DSN.
	// The default is to reject it (S10) because plaintext DB traffic is a
	// credible exfiltration channel in any non-loopback deployment.
	// Developers intentionally running the CP against a local docker
	// Postgres over 127.0.0.1 can set PANVEX_ALLOW_INSECURE_DB=1 to opt
	// back in. Unit tests that need plaintext DSN fixtures do the same.
	EnvAllowInsecureDB = "PANVEX_ALLOW_INSECURE_DB"
)

var (
	// ErrUnsupportedStorageDriver reports an unknown configured storage backend.
	ErrUnsupportedStorageDriver = errors.New("unsupported storage driver")
	// ErrStorageDSNRequired reports a missing DSN for a backend that has no safe default.
	ErrStorageDSNRequired = errors.New("storage dsn is required")
	// ErrInsecureDBDSN reports that the PostgreSQL DSN requests
	// sslmode=disable without PANVEX_ALLOW_INSECURE_DB set (S10).
	ErrInsecureDBDSN = errors.New("postgres dsn has sslmode=disable; set PANVEX_ALLOW_INSECURE_DB=1 to allow")
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

// ValidateStorageSecurity rejects insecure storage configurations the
// operator almost certainly did not intend. Called from the top-level
// config loaders (LoadControlPlaneConfig / ResolveLegacyControlPlaneConfig)
// after ResolveStorage, so ResolveStorage itself stays a pure normalizer
// that downstream unit tests can drive with any DSN shape.
//
// Currently enforced (S10): PostgreSQL DSN with sslmode=disable is refused
// unless PANVEX_ALLOW_INSECURE_DB is set. Matches both URL-form
// (?sslmode=disable) and keyword-form (... sslmode=disable ...) DSNs,
// case-insensitive.
func ValidateStorageSecurity(storage StorageConfig) error {
	if storage.Driver != StorageDriverPostgres {
		return nil
	}
	if !dsnHasSSLModeDisabled(storage.DSN) {
		return nil
	}
	if strings.TrimSpace(os.Getenv(EnvAllowInsecureDB)) == "" {
		return ErrInsecureDBDSN
	}
	return nil
}

func dsnHasSSLModeDisabled(dsn string) bool {
	lowered := strings.ToLower(dsn)
	// URL-form: parsed as a query-string token. Accept both ?sslmode=disable
	// and &sslmode=disable at token boundaries to avoid false positives on
	// passwords that happen to contain the substring.
	if strings.Contains(lowered, "?sslmode=disable") ||
		strings.Contains(lowered, "&sslmode=disable") {
		return true
	}
	// Keyword-form: space-delimited key=value pairs.
	for _, field := range strings.Fields(lowered) {
		if field == "sslmode=disable" {
			return true
		}
	}
	return false
}
