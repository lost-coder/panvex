package main

import (
	"errors"
	"testing"

	"github.com/lost-coder/panvex/internal/controlplane/config"
)

// TestServeRejectsInsecurePostgresDSNInProduction (A8): runServe must
// fail fast — BEFORE any connection attempt — when PANVEX_ENV=production
// and the Postgres DSN carries sslmode=disable. Prior to this change
// ValidateStorageSecurity was dead code and serve happily dialed the
// plaintext DSN.
func TestServeRejectsInsecurePostgresDSNInProduction(t *testing.T) {
	t.Setenv("PANVEX_ENV", "production")
	t.Setenv("PANVEX_STORAGE_DRIVER", "postgres")
	t.Setenv("PANVEX_STORAGE_DSN", "postgres://panvex:secret@127.0.0.1:1/panvex?sslmode=disable")
	// The production path ignores the escape hatch by design (S4);
	// set it anyway to prove the prod guard cannot be weakened.
	t.Setenv("PANVEX_ALLOW_INSECURE_DB", "1")
	// A dummy key is required for parseServeConfig to pass config
	// validation; the guard under test runs after config resolution
	// and before any connection attempt.
	t.Setenv("PANVEX_ENCRYPTION_KEY", "testonly-dummy-key-for-config-load")

	err := runServe(nil)
	if !errors.Is(err, config.ErrInsecureDBDSNProd) {
		t.Fatalf("runServe() error = %v, want config.ErrInsecureDBDSNProd", err)
	}
}
