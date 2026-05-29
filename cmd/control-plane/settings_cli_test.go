package main

import (
	"bytes"
	"io"
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

func TestSettingsCLI_GetUnknownErrors(t *testing.T) {
	dsn := newTempSQLite(t)
	err := runSettingsOut(io.Discard, []string{"get", "-storage-driver", "sqlite", "-storage-dsn", dsn, "no.such.key"})
	if err == nil {
		t.Fatal("get of unknown key should error")
	}
}

func TestSettingsCLI_GetOperationalDefault(t *testing.T) {
	dsn := newTempSQLite(t)
	var buf bytes.Buffer
	// http.public_url is operational with an empty default; get should succeed.
	if err := runSettingsOut(&buf, []string{"get", "-storage-driver", "sqlite", "-storage-dsn", dsn, "http.public_url"}); err != nil {
		t.Fatalf("get http.public_url: %v", err)
	}
}

func TestSettingsCLI_GetConfigManagedErrors(t *testing.T) {
	dsn := newTempSQLite(t)
	// storage.dsn is config/bootstrap-managed; get should report it is not in the DB.
	err := runSettingsOut(io.Discard, []string{"get", "-storage-driver", "sqlite", "-storage-dsn", dsn, "storage.dsn"})
	if err == nil {
		t.Fatal("get of a config-managed key should error")
	}
}

func TestSettingsCLI_SetRoundTrip(t *testing.T) {
	dsn := newTempSQLite(t)
	flagsdsn := []string{"-storage-driver", "sqlite", "-storage-dsn", dsn}
	if err := runSettingsOut(io.Discard, append([]string{"set"}, append(append([]string{}, flagsdsn...), "http.public_url", "https://cli.example")...)); err != nil {
		t.Fatalf("set: %v", err)
	}
	var buf bytes.Buffer
	if err := runSettingsOut(&buf, append([]string{"get"}, append(append([]string{}, flagsdsn...), "http.public_url")...)); err != nil {
		t.Fatalf("get: %v", err)
	}
	if got := strings.TrimSpace(buf.String()); got != "https://cli.example" {
		t.Fatalf("get after set = %q, want https://cli.example", got)
	}
}

func TestSettingsCLI_SetRejectsBootstrap(t *testing.T) {
	dsn := newTempSQLite(t)
	err := runSettingsOut(io.Discard, []string{"set", "-storage-driver", "sqlite", "-storage-dsn", dsn, "storage.dsn", "x"})
	if err == nil {
		t.Fatal("set of a config/bootstrap key should error")
	}
}

func TestSettingsCLI_Reset(t *testing.T) {
	dsn := newTempSQLite(t)
	f := []string{"-storage-driver", "sqlite", "-storage-dsn", dsn}
	if err := runSettingsOut(io.Discard, append([]string{"set"}, append(append([]string{}, f...), "http.listen_address", ":9999")...)); err != nil {
		t.Fatalf("set: %v", err)
	}
	if err := runSettingsOut(io.Discard, append([]string{"reset"}, append(append([]string{}, f...), "http.listen_address")...)); err != nil {
		t.Fatalf("reset: %v", err)
	}
	var buf bytes.Buffer
	if err := runSettingsOut(&buf, append([]string{"get"}, append(append([]string{}, f...), "http.listen_address")...)); err != nil {
		t.Fatalf("get: %v", err)
	}
	if got := strings.TrimSpace(buf.String()); got != ":8080" {
		t.Fatalf("after reset = %q, want :8080 (registry default)", got)
	}
}

func TestSettingsCLI_ResetAllAndKeyMutuallyExclusive(t *testing.T) {
	dsn := newTempSQLite(t)
	// `--all` is stripped before flag parse, then both `all` and `key` are set,
	// so the mutual-exclusion guard must fire.
	err := runSettingsOut(io.Discard, []string{"reset", "-storage-driver", "sqlite", "-storage-dsn", dsn, "--all", "http.listen_address"})
	if err == nil {
		t.Fatal("reset --all with a specific key should error (mutually exclusive)")
	}
}

func TestSettingsCLI_ResetAll(t *testing.T) {
	dsn := newTempSQLite(t)
	f := []string{"-storage-driver", "sqlite", "-storage-dsn", dsn}
	if err := runSettingsOut(io.Discard, append([]string{"set"}, append(append([]string{}, f...), "http.listen_address", ":9999")...)); err != nil {
		t.Fatalf("set: %v", err)
	}
	if err := runSettingsOut(io.Discard, append([]string{"reset"}, append(append([]string{}, f...), "--all")...)); err != nil {
		t.Fatalf("reset --all: %v", err)
	}
	var buf bytes.Buffer
	if err := runSettingsOut(&buf, append([]string{"get"}, append(append([]string{}, f...), "http.listen_address")...)); err != nil {
		t.Fatalf("get: %v", err)
	}
	if got := strings.TrimSpace(buf.String()); got != ":8080" {
		t.Fatalf("after reset --all = %q, want :8080 (registry default)", got)
	}
}
