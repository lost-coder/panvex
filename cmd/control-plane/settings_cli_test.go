package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

// newTempSQLite returns a DSN pointing at a fresh, migrated SQLite database in
// a per-test temp dir. openStore (via openSettingsStore) opens + migrates it.
func newTempSQLite(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "settings.db")
}

func TestSettingsCLI_List(t *testing.T) {
	dsn := newTempSQLite(t)
	// list runs without error and prints known keys plus source/tier info.
	var buf bytes.Buffer
	if err := runSettingsOut(&buf, []string{"list", "-storage-driver", "sqlite", "-storage-dsn", dsn}); err != nil {
		t.Fatalf("list: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "http.public_url") {
		t.Fatalf("list output missing http.public_url:\n%s", out)
	}
	// Bootstrap/config-managed key is shown as managed via env/config.
	if !strings.Contains(out, "storage.dsn") || !strings.Contains(out, "managed via env/config") {
		t.Fatalf("list output missing managed-via-env guidance for storage.dsn:\n%s", out)
	}
	// Operational keys show a source attribution.
	if !strings.Contains(out, "source=") {
		t.Fatalf("list output missing source attribution:\n%s", out)
	}
}
