package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/server"
	"github.com/lost-coder/panvex/internal/controlplane/storage/sqlite"
)

// seedExpiredCAForCLITest plants an expired CA record into the store so the
// CLI test has something to rotate.
func seedExpiredCAForCLITest(t *testing.T, dbPath string) {
	t.Helper()
	store, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatalf("sqlite.Open for seed: %v", err)
	}
	defer store.Close()
	past := time.Now().Add(-10 * 365 * 24 * time.Hour)
	if err := server.RotateCertificateAuthority(context.Background(), store, past, ""); err != nil {
		t.Fatalf("seed expired CA via RotateCertificateAuthority: %v", err)
	}
}

func TestRotateCA_WithConfirm_ReplacesRecord(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "panvex.db")

	seedExpiredCAForCLITest(t, dbPath)

	// Capture the CA PEM before rotation.
	storeB, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatalf("open store (before): %v", err)
	}
	before, err := storeB.GetCertificateAuthority(context.Background())
	storeB.Close()
	if err != nil {
		t.Fatalf("GetCertificateAuthority before: %v", err)
	}

	err = runRotateCA([]string{
		"--confirm",
		"--" + flagStorageDriver, "sqlite",
		"--" + flagStorageDSN, dbPath,
	})
	if err != nil {
		t.Fatalf("runRotateCA with --confirm: %v", err)
	}

	storeA, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatalf("open store (after): %v", err)
	}
	after, err := storeA.GetCertificateAuthority(context.Background())
	storeA.Close()
	if err != nil {
		t.Fatalf("GetCertificateAuthority after: %v", err)
	}

	if after.CAPEM == before.CAPEM {
		t.Fatal("rotate-ca must produce a new CA certificate")
	}
}

func TestRotateCA_WithoutConfirm_ReturnsError(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "panvex.db")

	seedExpiredCAForCLITest(t, dbPath)

	err := runRotateCA([]string{
		"--" + flagStorageDriver, "sqlite",
		"--" + flagStorageDSN, dbPath,
	})
	if err == nil {
		t.Fatal("runRotateCA without --confirm: err = nil, want error")
	}
	if !strings.Contains(err.Error(), "--confirm") {
		t.Errorf("error = %q, want mention of --confirm", err)
	}
}
