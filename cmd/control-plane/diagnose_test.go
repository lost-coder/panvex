package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/auth"
	"github.com/lost-coder/panvex/internal/controlplane/config"
	"github.com/lost-coder/panvex/internal/controlplane/storage/sqlite"
)

func sha256Prefix(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])[:8]
}

// TestDiagnoseCollectsHappyPath drives the diagnose collector against a
// freshly seeded SQLite store and asserts the high-signal sections are
// present in the rendered report.
func TestDiagnoseCollectsHappyPath(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "diagnose.db")
	store, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}

	// Seed an admin user so the row-counts row is non-zero. Mirrors the
	// bootstrap-admin path used in main_test.go fixtures.
	svc := auth.NewServiceWithStore(store)
	if _, _, err := svc.BootstrapUser(context.Background(), auth.BootstrapInput{
		Username: "admin",
		Password: "supersecretpw1",
		Role:     auth.RoleAdmin,
	}, time.Now()); err != nil {
		t.Fatalf("BootstrapUser: %v", err)
	}
	store.Close()

	t.Setenv("PANVEX_ENCRYPTION_KEY", "fingerprint-source")

	report, err := collectDiagnostics(context.Background(), config.StorageConfig{
		Driver: config.StorageDriverSQLite,
		DSN:    dbPath,
	})
	if err != nil {
		t.Fatalf("collectDiagnostics: %v", err)
	}

	mustContain(t, report, "# Panvex control-plane diagnose")
	mustContain(t, report, "| Storage driver | sqlite |")
	mustContain(t, report, "| Users | 1 |")
	mustContain(t, report, "| Agents | 0 |")
	mustContain(t, report, "| Jobs (total) | 0 |")
	// First 8 hex chars of SHA256("fingerprint-source"). Computed via
	// crypto/sha256 inside the test rather than a hard-coded literal
	// so the assertion fails for the right reason if the helper is
	// later widened — and so we never risk pinning the wrong value.
	wantFP := sha256Prefix("fingerprint-source")
	mustContain(t, report, "| Encryption key fingerprint | "+wantFP+" |")
	mustContain(t, report, "| Schema version |")
	// CA is not yet initialised on a brand-new store; collector
	// should report this rather than failing.
	mustContain(t, report, "(no CA initialised)")
}

// TestDiagnoseRejectsUnopenableDSN verifies that store-open errors
// surface to the operator as the wrapped "open store" error rather
// than a half-rendered report. We use ":memory:" because the SQLite
// adapter explicitly rejects in-memory DSNs (WAL needs a real file)
// — that gives us a deterministic Open failure across platforms
// without needing to fiddle with directory permissions.
func TestDiagnoseRejectsUnopenableDSN(t *testing.T) {
	_, err := collectDiagnostics(context.Background(), config.StorageConfig{
		Driver: config.StorageDriverSQLite,
		DSN:    ":memory:",
	})
	if err == nil {
		t.Fatalf("expected error for in-memory DSN, got nil")
	}
	if !strings.Contains(err.Error(), "open store") {
		t.Fatalf("expected 'open store' wrap, got: %v", err)
	}
}

// TestMaskDSNRedactsPostgresPassword pins the redaction logic so a
// regression that prints the password to support tickets fails fast.
func TestMaskDSNRedactsPostgresPassword(t *testing.T) {
	masked := maskDSN("postgres://panvex:supersecret@db.internal:5432/panvex")
	if strings.Contains(masked, "supersecret") {
		t.Fatalf("maskDSN leaked password: %q", masked)
	}
	if !strings.Contains(masked, "panvex@db.internal") && !strings.Contains(masked, "xxxxx@") {
		t.Fatalf("maskDSN did not redact userinfo: %q", masked)
	}
}

// TestEncryptionFingerprintOmitsRawKey is a paranoia test: the
// fingerprint helper is the boundary that must never leak the key
// itself. If a future change accidentally returns the plaintext
// (or even a substring of it) this pins the contract.
func TestEncryptionFingerprintOmitsRawKey(t *testing.T) {
	t.Setenv("PANVEX_ENCRYPTION_KEY", "raw-secret-do-not-leak")
	row := encryptionFingerprintRow()
	if strings.Contains(row.Value, "raw-secret") {
		t.Fatalf("fingerprint row leaked key material: %q", row.Value)
	}
	if len(row.Value) != 8 {
		t.Fatalf("fingerprint length = %d, want 8", len(row.Value))
	}
}

// TestEncryptionFingerprintUnsetReportsExplicitly avoids confusing
// operators with an empty cell when PANVEX_ENCRYPTION_KEY isn't set.
func TestEncryptionFingerprintUnsetReportsExplicitly(t *testing.T) {
	t.Setenv("PANVEX_ENCRYPTION_KEY", "")
	row := encryptionFingerprintRow()
	if row.Value != "(unset)" {
		t.Fatalf("expected (unset) for empty key, got %q", row.Value)
	}
}

func mustContain(t *testing.T, haystack, needle string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Fatalf("report missing %q.\n--- report ---\n%s", needle, haystack)
	}
}
