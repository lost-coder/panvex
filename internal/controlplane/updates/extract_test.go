package updates

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeTarGzArchive builds a .tar.gz file at path from the given entries,
// in order. Each entry becomes one tar header + body.
type tarEntry struct {
	name     string
	typeflag byte
	size     int64
	body     []byte
	linkname string
}

func writeTarGzArchive(t *testing.T, path string, entries []tarEntry) {
	t.Helper()

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	for _, e := range entries {
		hdr := &tar.Header{
			Name:     e.name,
			Typeflag: e.typeflag,
			Size:     e.size,
			Mode:     0755,
			Linkname: e.linkname,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("WriteHeader(%q): %v", e.name, err)
		}
		if e.typeflag == tar.TypeReg && len(e.body) > 0 {
			if _, err := tw.Write(e.body); err != nil {
				t.Fatalf("Write(%q): %v", e.name, err)
			}
		}
	}

	if err := tw.Close(); err != nil {
		t.Fatalf("tar Close: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("gzip Close: %v", err)
	}

	if err := os.WriteFile(path, buf.Bytes(), 0644); err != nil { //nolint:gosec // test archive
		t.Fatalf("WriteFile: %v", err)
	}
}

func TestExtractBinaryFromArchiveValidRegularFile(t *testing.T) {
	dir := t.TempDir()
	archivePath := filepath.Join(dir, "archive.tar.gz")
	content := []byte("#!/bin/sh\necho hello\n")
	writeTarGzArchive(t, archivePath, []tarEntry{
		{name: "control-plane", typeflag: tar.TypeReg, size: int64(len(content)), body: content},
	})

	binPath, err := ExtractBinaryFromArchive(archivePath)
	if err != nil {
		t.Fatalf("ExtractBinaryFromArchive() error = %v", err)
	}
	defer func() { _ = os.Remove(binPath) }()

	got, err := os.ReadFile(binPath)
	if err != nil {
		t.Fatalf("ReadFile(extracted): %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Fatalf("extracted content = %q, want %q", got, content)
	}
}

func TestExtractBinaryFromArchiveOversizedEntryRejected(t *testing.T) {
	dir := t.TempDir()
	archivePath := filepath.Join(dir, "archive.tar.gz")
	content := bytes.Repeat([]byte("A"), maxBinarySize+1)
	writeTarGzArchive(t, archivePath, []tarEntry{
		{name: "control-plane", typeflag: tar.TypeReg, size: int64(len(content)), body: content},
	})

	_, err := ExtractBinaryFromArchive(archivePath)
	if err == nil {
		t.Fatal("ExtractBinaryFromArchive() error = nil, want error for oversized entry")
	}
}

func TestExtractBinaryFromArchiveDirectoryEntrySkipped(t *testing.T) {
	dir := t.TempDir()
	archivePath := filepath.Join(dir, "archive.tar.gz")
	content := []byte("real-binary-content")
	writeTarGzArchive(t, archivePath, []tarEntry{
		{name: "some-dir/", typeflag: tar.TypeDir},
		{name: "control-plane", typeflag: tar.TypeReg, size: int64(len(content)), body: content},
	})

	binPath, err := ExtractBinaryFromArchive(archivePath)
	if err != nil {
		t.Fatalf("ExtractBinaryFromArchive() error = %v, want directory entry skipped and regular file extracted", err)
	}
	defer func() { _ = os.Remove(binPath) }()

	got, err := os.ReadFile(binPath)
	if err != nil {
		t.Fatalf("ReadFile(extracted): %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Fatalf("extracted content = %q, want %q", got, content)
	}
}

func TestExtractBinaryFromArchiveOnlyDirectoryRejected(t *testing.T) {
	dir := t.TempDir()
	archivePath := filepath.Join(dir, "archive.tar.gz")
	writeTarGzArchive(t, archivePath, []tarEntry{
		{name: "some-dir/", typeflag: tar.TypeDir},
	})

	_, err := ExtractBinaryFromArchive(archivePath)
	if err == nil {
		t.Fatal("ExtractBinaryFromArchive() error = nil, want error when archive has no regular-file entry")
	}
	// The io.EOF path must surface an actionable message, not a bare
	// "read tar entry: EOF".
	if !strings.Contains(err.Error(), "no regular file entries") {
		t.Fatalf("ExtractBinaryFromArchive() error = %q, want it to mention 'no regular file entries'", err)
	}
}

func TestExtractBinaryFromArchiveSymlinkEntrySkipped(t *testing.T) {
	dir := t.TempDir()
	archivePath := filepath.Join(dir, "archive.tar.gz")
	content := []byte("real-binary-content")
	writeTarGzArchive(t, archivePath, []tarEntry{
		{name: "control-plane-link", typeflag: tar.TypeSymlink, linkname: "control-plane"},
		{name: "control-plane", typeflag: tar.TypeReg, size: int64(len(content)), body: content},
	})

	binPath, err := ExtractBinaryFromArchive(archivePath)
	if err != nil {
		t.Fatalf("ExtractBinaryFromArchive() error = %v, want symlink entry skipped and regular file extracted", err)
	}
	defer func() { _ = os.Remove(binPath) }()

	got, err := os.ReadFile(binPath)
	if err != nil {
		t.Fatalf("ReadFile(extracted): %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Fatalf("extracted content = %q, want %q", got, content)
	}
}

func TestExtractBinaryFromArchiveEmptyArchiveRejected(t *testing.T) {
	dir := t.TempDir()
	archivePath := filepath.Join(dir, "archive.tar.gz")
	writeTarGzArchive(t, archivePath, nil)

	_, err := ExtractBinaryFromArchive(archivePath)
	if err == nil {
		t.Fatal("ExtractBinaryFromArchive() error = nil, want error for empty archive")
	}
}

func TestExtractBinaryFromArchiveZeroByteEntryRejected(t *testing.T) {
	dir := t.TempDir()
	archivePath := filepath.Join(dir, "archive.tar.gz")
	writeTarGzArchive(t, archivePath, []tarEntry{
		{name: "control-plane", typeflag: tar.TypeReg, size: 0},
	})

	_, err := ExtractBinaryFromArchive(archivePath)
	if err == nil {
		t.Fatal("ExtractBinaryFromArchive() error = nil, want error for zero-byte entry")
	}
}
