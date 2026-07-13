package updates

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
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

// R1.5 (audit 2026-07-07 §1.14): a garbage checksum file must fail at
// parse time with a clear error, not later as an opaque
// "checksum mismatch" against a non-hex "digest".
func TestIsHexSHA256(t *testing.T) {
	if !isHexSHA256(strings.Repeat("ab12", 16)) {
		t.Fatal("valid 64-hex digest rejected")
	}
	if !isHexSHA256(strings.Repeat("AB12", 16)) {
		t.Fatal("uppercase hex digest rejected")
	}
	for _, s := range []string{
		"",
		"not-a-digest",
		strings.Repeat("a", 63),
		strings.Repeat("a", 65),
		strings.Repeat("g", 64),
	} {
		if isHexSHA256(s) {
			t.Errorf("isHexSHA256(%q) = true, want false", s)
		}
	}
}
