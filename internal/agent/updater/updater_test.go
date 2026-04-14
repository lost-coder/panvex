package updater

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestReplaceBinary(t *testing.T) {
	dir := t.TempDir()
	currentPath := filepath.Join(dir, "panvex-agent")
	os.WriteFile(currentPath, []byte("old-binary"), 0755)

	newContent := []byte("new-binary")
	hash := sha256.Sum256(newContent)
	checksum := hex.EncodeToString(hash[:])

	newPath := filepath.Join(dir, "panvex-agent-new")
	os.WriteFile(newPath, newContent, 0755)

	err := replaceBinary(currentPath, newPath, checksum)
	if err != nil {
		t.Fatalf("replaceBinary() error = %v", err)
	}
	got, _ := os.ReadFile(currentPath)
	if string(got) != "new-binary" {
		t.Fatalf("content = %q, want %q", got, "new-binary")
	}
}
