package updater

import (
	"archive/tar"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

func TestExtractAndReplace(t *testing.T) {
	dir := t.TempDir()

	// Create a tar.gz archive with a single binary inside.
	archivePath := filepath.Join(dir, "panvex-agent-linux-amd64.tar.gz")
	binaryContent := []byte("new-binary-content")
	createTestArchive(t, archivePath, "panvex-agent-linux-amd64", binaryContent)

	// Extract binary from archive.
	binaryPath, err := extractBinaryFromArchive(archivePath)
	if err != nil {
		t.Fatalf("extractBinaryFromArchive() error = %v", err)
	}
	defer func() { _ = os.Remove(binaryPath) }()

	got, err := os.ReadFile(binaryPath)
	if err != nil {
		t.Fatalf("read extracted binary: %v", err)
	}
	if string(got) != string(binaryContent) {
		t.Fatalf("extracted content = %q, want %q", got, binaryContent)
	}

	// Test replaceSelf.
	currentPath := filepath.Join(dir, "panvex-agent")
	if err := os.WriteFile(currentPath, []byte("old-binary"), 0755); err != nil { //nolint:gosec // test binary
		t.Fatal(err)
	}

	if err := replaceSelf(currentPath, binaryPath); err != nil {
		t.Fatalf("replaceSelf() error = %v", err)
	}

	replaced, _ := os.ReadFile(currentPath)
	if string(replaced) != string(binaryContent) {
		t.Fatalf("replaced content = %q, want %q", replaced, binaryContent)
	}

	backup, _ := os.ReadFile(currentPath + ".bak")
	if string(backup) != "old-binary" {
		t.Fatalf("backup content = %q, want %q", backup, "old-binary")
	}
}

func createTestArchive(t *testing.T, archivePath, entryName string, content []byte) {
	t.Helper()
	f, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	gw := gzip.NewWriter(f)
	defer gw.Close()

	tw := tar.NewWriter(gw)
	defer tw.Close()

	if err := tw.WriteHeader(&tar.Header{
		Name: entryName,
		Size: int64(len(content)),
		Mode: 0755,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatal(err)
	}
}
