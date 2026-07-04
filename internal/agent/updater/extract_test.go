package updater

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type tarEntry struct {
	name     string
	body     []byte
	typeflag byte
	// sizeOverride, when > 0, lies in the tar header about the body size
	// (crafts a truncated entry).
	sizeOverride int64
}

func makeTarGz(t *testing.T, entries []tarEntry) string {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		typeflag := e.typeflag
		if typeflag == 0 {
			typeflag = tar.TypeReg
		}
		size := int64(len(e.body))
		if e.sizeOverride > 0 {
			size = e.sizeOverride
		}
		if err := tw.WriteHeader(&tar.Header{
			Name:     e.name,
			Mode:     0o755,
			Size:     size,
			Typeflag: typeflag,
		}); err != nil {
			t.Fatalf("write header %s: %v", e.name, err)
		}
		if _, err := tw.Write(e.body); err != nil && e.sizeOverride == 0 {
			t.Fatalf("write body %s: %v", e.name, err)
		}
	}
	// Deliberately ignore Close errors: the truncated-entry case leaves
	// the writer in an inconsistent state by design.
	_ = tw.Close()
	_ = gz.Close()

	path := filepath.Join(t.TempDir(), "archive.tar.gz")
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatalf("write archive: %v", err)
	}
	return path
}

func TestExtractSkipsLeadingEntriesAndFindsBinary(t *testing.T) {
	binary := []byte("#!/bin/true\nfake-binary-payload")
	archive := makeTarGz(t, []tarEntry{
		{name: "README.md", body: []byte("docs first")},
		{name: "panvex-agent-test/", typeflag: tar.TypeDir},
		{name: "panvex-agent-test", body: binary},
	})

	got, err := extractBinaryFromArchive(archive, "panvex-agent-test")
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	defer func() { _ = os.Remove(got) }()
	data, err := os.ReadFile(got)
	if err != nil {
		t.Fatalf("read extracted: %v", err)
	}
	if !bytes.Equal(data, binary) {
		t.Fatalf("extracted %d bytes != expected binary %d bytes", len(data), len(binary))
	}
	info, err := os.Stat(got)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm()&0o100 == 0 {
		t.Fatal("extracted binary is not executable")
	}
}

func TestExtractFailsWhenBinaryMissing(t *testing.T) {
	archive := makeTarGz(t, []tarEntry{
		{name: "README.md", body: []byte("no binary here")},
	})
	_, err := extractBinaryFromArchive(archive, "panvex-agent-test")
	if err == nil || !strings.Contains(err.Error(), "does not contain") {
		t.Fatalf("err = %v, want 'archive does not contain ...'", err)
	}
}

func TestExtractFailsOnTruncatedBinary(t *testing.T) {
	archive := makeTarGz(t, []tarEntry{
		{name: "panvex-agent-test", body: []byte("short"), sizeOverride: 4096},
	})
	_, err := extractBinaryFromArchive(archive, "panvex-agent-test")
	if err == nil {
		t.Fatal("extract of a truncated entry succeeded, want error")
	}
	if !strings.Contains(err.Error(), "extract binary") {
		t.Fatalf("err = %v, want an 'extract binary' failure", err)
	}
}
