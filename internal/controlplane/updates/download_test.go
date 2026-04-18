package updates

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestVerifyChecksum(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "binary")
	content := []byte("test binary content")
	if err := os.WriteFile(path, content, 0755); err != nil { //nolint:gosec // test binary
		t.Fatal(err)
	}

	hash := sha256.Sum256(content)
	expected := hex.EncodeToString(hash[:])

	if err := VerifyChecksum(path, expected); err != nil {
		t.Fatalf("VerifyChecksum() error = %v", err)
	}
	if err := VerifyChecksum(path, "0000000000000000000000000000000000000000000000000000000000000000"); err == nil {
		t.Fatal("VerifyChecksum() error = nil, want mismatch error")
	}
}

func TestAtomicReplaceBinary(t *testing.T) {
	dir := t.TempDir()
	currentPath := filepath.Join(dir, "current")
	newPath := filepath.Join(dir, "new")
	if err := os.WriteFile(currentPath, []byte("old"), 0755); err != nil { //nolint:gosec // test binary
		t.Fatal(err)
	}
	if err := os.WriteFile(newPath, []byte("new"), 0755); err != nil { //nolint:gosec // test binary
		t.Fatal(err)
	}

	if err := AtomicReplaceBinary(currentPath, newPath); err != nil {
		t.Fatalf("AtomicReplaceBinary() error = %v", err)
	}
	got, _ := os.ReadFile(currentPath)
	if string(got) != "new" {
		t.Fatalf("content = %q, want %q", got, "new")
	}
	backup, _ := os.ReadFile(currentPath + ".bak")
	if string(backup) != "old" {
		t.Fatalf("backup = %q, want %q", backup, "old")
	}
}
