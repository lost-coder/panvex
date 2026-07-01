package config

import (
	"errors"
	"fmt"
	"net/url"
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
	// ErrEmptyPostgresPassword reports that the PostgreSQL DSN omits a
	// password and PANVEX_DB_PASSWORD is also unset. Closes Q1.U-S-13:
	// dev-fixtures with empty passwords must not silently leak into a
	// prod start. PANVEX_ALLOW_EMPTY_DB_PASSWORD=1 escapes this for
	// loopback-only fixtures and tests.
	ErrEmptyPostgresPassword = errors.New("postgres dsn has empty password; set PANVEX_DB_PASSWORD or PANVEX_ALLOW_EMPTY_DB_PASSWORD=1 for dev")
	// ErrInsecureDBDSNProd reports that the PostgreSQL DSN requests
	// sslmode=disable while PANVEX_ENV=production. In production the
	// PANVEX_ALLOW_INSECURE_DB escape hatch is intentionally ignored, so a
	// dev configuration cannot be started against prod (S4).
	ErrInsecureDBDSNProd = errors.New("postgres dsn has sslmode=disable; PANVEX_ALLOW_INSECURE_DB is ignored when PANVEX_ENV=production")
	// ErrEmptyPostgresPasswordProd reports that the PostgreSQL DSN omits a
	// password while PANVEX_ENV=production. In production the
	// PANVEX_ALLOW_EMPTY_DB_PASSWORD escape hatch is intentionally ignored
	// (S4).
	ErrEmptyPostgresPasswordProd = errors.New("postgres dsn has empty password; PANVEX_ALLOW_EMPTY_DB_PASSWORD is ignored when PANVEX_ENV=production")

	// EnvAllowEmptyDBPassword opts into accepting a PostgreSQL DSN with
	// no password embedded and no PANVEX_DB_PASSWORD env. Default is to
	// reject because dev-compose fixtures with empty creds occasionally
	// reach prod via copy-paste.
	EnvAllowEmptyDBPassword = "PANVEX_ALLOW_EMPTY_DB_PASSWORD" //nolint:gosec // env var name, not a credential
)

// StorageConfig describes the selected persistent storage backend.
type StorageConfig struct {
	Driver string
	DSN    string
}

// ResolveStorage normalizes storage backend input and applies safe defaults.
func ResolveStorage(driver, dsn string) (StorageConfig, error) {
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
// operator almost certainly did not intend. Called after ResolveStorage,
// so ResolveStorage itself stays a pure normalizer that downstream unit
// tests can drive with any DSN shape.
//
// Currently enforced (S10): PostgreSQL DSN with sslmode=disable is refused
// unless PANVEX_ALLOW_INSECURE_DB is set. Matches both URL-form
// (?sslmode=disable) and keyword-form (... sslmode=disable ...) DSNs,
// case-insensitive.
func ValidateStorageSecurity(storage StorageConfig) error {
	if storage.Driver != StorageDriverPostgres {
		return nil
	}
	prod := isProductionEnv()
	// A Unix-socket DSN never puts DB traffic on the network, so the
	// sslmode=disable guard — which exists to stop plaintext exfiltration
	// over a network channel — does not apply. TLS over a local socket is
	// meaningless, and Postgres rejects sslmode=require there anyway. This
	// is the recommended single-host topology (deploy/docker-compose.prod.yml).
	if dsnHasSSLModeDisabled(storage.DSN) && !dsnUsesUnixSocket(storage.DSN) {
		if prod {
			// S4: the dev-loopback opt-in must not weaken a prod start.
			return ErrInsecureDBDSNProd
		}
		if strings.TrimSpace(os.Getenv(EnvAllowInsecureDB)) == "" {
			return ErrInsecureDBDSN
		}
	}
	if dsnHasEmptyPostgresPassword(storage.DSN) && strings.TrimSpace(os.Getenv(EnvDBPassword)) == "" {
		if prod {
			// S4: the empty-password escape hatch must not weaken a prod start.
			return ErrEmptyPostgresPasswordProd
		}
		if strings.TrimSpace(os.Getenv(EnvAllowEmptyDBPassword)) == "" {
			return ErrEmptyPostgresPassword
		}
	}
	return nil
}

// isProductionEnv reports whether PANVEX_ENV selects the production
// environment, case-insensitively. In production the insecure-DB escape
// hatches (PANVEX_ALLOW_INSECURE_DB, PANVEX_ALLOW_EMPTY_DB_PASSWORD) are
// ignored so a dev configuration cannot be started in prod (S4).
func isProductionEnv() bool {
	return IsProductionEnv()
}

// IsProductionEnv reports whether PANVEX_ENV selects the production
// environment, case-insensitively. Exported so other packages (e.g.
// controlplane/server's trusted-proxy misconfiguration guard) can gate
// their own fail-loud-in-prod checks on the exact same signal this package
// uses for the storage-security guards, instead of re-reading the env var
// with a second, potentially-drifting implementation.
func IsProductionEnv() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv("PANVEX_ENV")), "production")
}

// dsnHasEmptyPostgresPassword reports whether the DSN has a userinfo
// section that explicitly carries no password, OR no userinfo at all
// (which still lets pgx/pq fall back to peer/trust auth — fine for
// loopback dev but unsafe for prod). The check is conservative: we
// only flag URL-form DSNs because keyword-form keeps the credentials
// elsewhere (PGPASSWORD, .pgpass) which we cannot reliably inspect.
func dsnHasEmptyPostgresPassword(dsn string) bool {
	trimmed := strings.TrimSpace(dsn)
	if !strings.Contains(trimmed, "://") {
		// Keyword form — PGPASSWORD or .pgpass may carry the secret;
		// we cannot tell, so do not block.
		return false
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return false
	}
	if parsed.User == nil {
		// postgres://host/db — no userinfo at all. Treat as empty.
		return true
	}
	password, hasPassword := parsed.User.Password()
	if !hasPassword {
		return true
	}
	return strings.TrimSpace(password) == ""
}

// dsnUsesUnixSocket reports whether the DSN targets a PostgreSQL Unix
// domain socket rather than a TCP host. pgx/libpq interpret a host that is
// an absolute filesystem path as a socket directory — supplied either as
// the URL authority (rare, percent-encoded) or, more commonly, as a
// host=/path query parameter on a URL DSN or a host=/path field on a
// keyword DSN. Socket connections never traverse the network, so the
// plaintext-DSN guard does not apply to them.
func dsnUsesUnixSocket(dsn string) bool {
	trimmed := strings.TrimSpace(dsn)
	if trimmed == "" {
		return false
	}
	if strings.Contains(trimmed, "://") {
		parsed, err := url.Parse(trimmed)
		if err != nil {
			return false
		}
		if strings.HasPrefix(parsed.Hostname(), "/") {
			return true
		}
		return strings.HasPrefix(strings.TrimSpace(parsed.Query().Get("host")), "/")
	}
	// Keyword form: host=/var/run/postgresql among space-delimited pairs.
	for _, field := range strings.Fields(trimmed) {
		if strings.HasPrefix(field, "host=/") {
			return true
		}
	}
	return false
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
