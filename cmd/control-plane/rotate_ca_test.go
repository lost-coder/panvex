package main

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/kdf"
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
	// These CLI tests exercise rotate-ca without an encryption key — opt
	// into the 3.1 plaintext-CA escape hatch rather than plumbing a real
	// passphrase through flags just to seed a fixture.
	t.Setenv(server.EnvAllowPlaintextCA, "1")
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

// TestRotateCA_HonoursKDFProfile: rotate-ca WRITES a fresh ENC3 CA blob, so it
// must honour PANVEX_KDF_PROFILE like serve and bootstrap-admin do. Otherwise a
// low-memory install that rotates its CA gets 96 MiB parameters baked into the
// blob header — and because ENC3 is never re-encrypted by design, every later
// startup pays that 96 MiB derivation forever, silently undoing the profile.
func TestRotateCA_HonoursKDFProfile(t *testing.T) {
	t.Setenv("PANVEX_ENCRYPTION_KEY", "rotate-ca-kdf-profile-test-passphrase")
	t.Setenv("PANVEX_KDF_PROFILE", "low-memory")
	t.Cleanup(func() { _ = kdf.SetActiveProfile("") })

	dbPath := filepath.Join(t.TempDir(), "panvex.db")
	seedExpiredCAForCLITest(t, dbPath)

	if err := runRotateCA([]string{
		"--confirm",
		"--" + flagStorageDriver, "sqlite",
		"--" + flagStorageDSN, dbPath,
	}); err != nil {
		t.Fatalf("runRotateCA with --confirm: %v", err)
	}

	store, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatalf("open store (after): %v", err)
	}
	defer store.Close()
	record, err := store.GetCertificateAuthority(context.Background())
	if err != nil {
		t.Fatalf("GetCertificateAuthority after: %v", err)
	}

	const enc3Prefix = "ENC3:"
	if !strings.HasPrefix(record.PrivateKeyPEM, enc3Prefix) {
		t.Fatalf("rotated key must be stored as ENC3, got %.16q", record.PrivateKeyPEM)
	}
	blob, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(record.PrivateKeyPEM, enc3Prefix))
	if err != nil {
		t.Fatalf("decode ENC3 blob: %v", err)
	}
	if len(blob) < 9 {
		t.Fatalf("ENC3 blob too short: %d bytes", len(blob))
	}
	// Header layout: t(4B BE) || m(4B BE) || p(1B).
	gotIter := binary.BigEndian.Uint32(blob[0:4])
	gotMem := binary.BigEndian.Uint32(blob[4:8])
	gotPar := blob[8]
	want := kdf.LowMemory
	if gotIter != want.Time || gotMem != want.MemoryKiB || gotPar != want.Threads {
		t.Fatalf("ENC3 header = t=%d,m=%d,p=%d, want the low-memory profile t=%d,m=%d,p=%d",
			gotIter, gotMem, gotPar, want.Time, want.MemoryKiB, want.Threads)
	}
}

// TestRotateCA_RejectsUnknownKDFProfile: a typo must fail before the CA is
// replaced — rotating invalidates the whole fleet, so a half-understood
// environment must not proceed.
func TestRotateCA_RejectsUnknownKDFProfile(t *testing.T) {
	t.Setenv("PANVEX_ENCRYPTION_KEY", "rotate-ca-kdf-profile-test-passphrase")
	t.Setenv("PANVEX_KDF_PROFILE", "lowmem")
	t.Cleanup(func() { _ = kdf.SetActiveProfile("") })

	dbPath := filepath.Join(t.TempDir(), "panvex.db")
	seedExpiredCAForCLITest(t, dbPath)

	err := runRotateCA([]string{
		"--confirm",
		"--" + flagStorageDriver, "sqlite",
		"--" + flagStorageDSN, dbPath,
	})
	if err == nil {
		t.Fatal("unknown PANVEX_KDF_PROFILE must fail the command")
	}
	if !strings.Contains(err.Error(), "PANVEX_KDF_PROFILE") {
		t.Fatalf("error must name the offending env var, got %v", err)
	}
}
