package main

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/auth"
	"github.com/lost-coder/panvex/internal/controlplane/storage/sqlite"
)

// TestBackupHappyPath drives the SQLite backup against a seeded store
// and verifies the resulting archive contains both the snapshot DB
// and a valid metadata.json with the expected fingerprint.
func TestBackupHappyPath(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "panvex.db")
	store, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatalf("sqlite.Open: %v", err)
	}
	svc := auth.NewServiceWithStore(store)
	if _, _, err := svc.BootstrapUser(context.Background(), auth.BootstrapInput{
		Username: "admin",
		Password: "supersecretpw1",
		Role:     auth.RoleAdmin,
	}, time.Now()); err != nil {
		t.Fatalf("BootstrapUser: %v", err)
	}
	store.Close()

	t.Setenv("PANVEX_ENCRYPTION_KEY", "backup-fingerprint-source")
	out := filepath.Join(t.TempDir(), "panvex.tar.gz")
	if err := writeSQLiteBackup(context.Background(), dbPath, out); err != nil {
		t.Fatalf("writeSQLiteBackup: %v", err)
	}

	entries := readArchive(t, out)
	if _, ok := entries["panvex.db"]; !ok {
		t.Fatalf("archive missing panvex.db; got entries: %v", keys(entries))
	}
	metaBytes, ok := entries["metadata.json"]
	if !ok {
		t.Fatalf("archive missing metadata.json; got entries: %v", keys(entries))
	}

	var meta backupMetadata
	if err := json.Unmarshal(metaBytes, &meta); err != nil {
		t.Fatalf("unmarshal metadata: %v", err)
	}
	if meta.StorageDriver != "sqlite" {
		t.Fatalf("metadata.StorageDriver = %q, want sqlite", meta.StorageDriver)
	}
	if meta.SchemaVersion <= 0 {
		t.Fatalf("metadata.SchemaVersion = %d, want >0 (migrations should be applied)", meta.SchemaVersion)
	}
	if meta.EncryptionKeyFingerprint == "" {
		t.Fatalf("metadata.EncryptionKeyFingerprint empty; expected 8-char prefix")
	}
	if len(meta.EncryptionKeyFingerprint) != 8 {
		t.Fatalf("fingerprint length = %d, want 8", len(meta.EncryptionKeyFingerprint))
	}
	if strings.Contains(meta.EncryptionKeyFingerprint, "backup-fingerprint") {
		t.Fatalf("fingerprint leaked raw key material: %q", meta.EncryptionKeyFingerprint)
	}

	// Verify the snapshot DB is itself a valid SQLite file by opening
	// it. Round-tripping the snapshot is the strongest way to catch a
	// truncated VACUUM INTO output.
	restorePath := filepath.Join(t.TempDir(), "restored.db")
	if err := os.WriteFile(restorePath, entries["panvex.db"], 0o600); err != nil {
		t.Fatalf("write restored db: %v", err)
	}
	restored, err := sqlite.Open(restorePath)
	if err != nil {
		t.Fatalf("open restored db: %v", err)
	}
	defer restored.Close()
	users, err := restored.ListUsers(context.Background())
	if err != nil {
		t.Fatalf("list users from restored db: %v", err)
	}
	if len(users) != 1 || users[0].Username != "admin" {
		t.Fatalf("restored users = %+v, want one admin", users)
	}
}

// TestBackupRefusesPostgres pins the contract that backup is
// SQLite-only: postgres operators should be redirected to pg_dump
// instead of getting a half-broken archive.
func TestBackupRefusesPostgres(t *testing.T) {
	err := runBackup([]string{
		"-storage-driver", "postgres",
		"-storage-dsn", "postgres://x:y@localhost/postgres",
		"-out", filepath.Join(t.TempDir(), "out.tar.gz"),
	})
	if err == nil {
		t.Fatalf("expected error refusing postgres backup")
	}
	if !strings.Contains(err.Error(), "sqlite") || !strings.Contains(err.Error(), "pg_dump") {
		t.Fatalf("error should mention sqlite-only and pg_dump, got: %v", err)
	}
}

// TestBackupRequiresOutFlag covers the operator typo case where -out
// is missing — we'd rather a clear error than silently writing into
// the cwd.
func TestBackupRequiresOutFlag(t *testing.T) {
	err := runBackup([]string{
		"-storage-driver", "sqlite",
		"-storage-dsn", filepath.Join(t.TempDir(), "x.db"),
	})
	if err == nil {
		t.Fatalf("expected error for missing -out")
	}
	if !strings.Contains(err.Error(), "-out") {
		t.Fatalf("error should mention -out, got: %v", err)
	}
}

// TestRestoreStubPrintsManualSteps ensures the discoverable
// `restore` command name does not silently no-op: it must print
// the manual recipe so operators can follow it.
func TestRestoreStubPrintsManualSteps(t *testing.T) {
	help := restoreHelpText()
	for _, want := range []string{"systemctl stop", "tar -xzf", "migrate-schema"} {
		if !strings.Contains(help, want) {
			t.Fatalf("restore help missing %q\n--- help ---\n%s", want, help)
		}
	}
}

// TestRunRestoreSucceeds ensures the dispatcher path returns nil and
// does not require any flags.
func TestRunRestoreSucceeds(t *testing.T) {
	if err := runRestore(nil); err != nil {
		t.Fatalf("runRestore: %v", err)
	}
}

// readArchive opens path as a tar.gz and returns each entry's bytes
// keyed by name. Used by TestBackupHappyPath to inspect the archive
// without reaching into archive/tar in every assertion.
func readArchive(t *testing.T, path string) map[string][]byte {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open archive: %v", err)
	}
	defer f.Close()
	gzr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	defer gzr.Close()
	tr := tar.NewReader(gzr)
	out := map[string][]byte{}
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("tar next: %v", err)
		}
		buf, err := io.ReadAll(tr)
		if err != nil {
			t.Fatalf("tar read %s: %v", hdr.Name, err)
		}
		out[hdr.Name] = buf
	}
	return out
}

func keys(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
