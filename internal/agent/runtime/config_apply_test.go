package runtime

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBackupAndRestoreConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("original = true\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	backup, err := backupConfigFile(path)
	if err != nil {
		t.Fatalf("backupConfigFile: %v", err)
	}
	if backup == "" {
		t.Fatal("expected non-empty backup path")
	}

	// Simulate a bad write.
	if err := os.WriteFile(path, []byte("broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := restoreConfigFile(backup, path); err != nil {
		t.Fatalf("restoreConfigFile: %v", err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "original = true\n" {
		t.Fatalf("restore mismatch: %q", got)
	}
}
