package updater

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestExtractAndReplace(t *testing.T) {
	dir := t.TempDir()

	// Create a tar.gz archive with a single binary inside.
	archivePath := filepath.Join(dir, "panvex-agent-linux-amd64.tar.gz")
	binaryContent := []byte("new-binary-content")
	createTestArchive(t, archivePath, "panvex-agent-linux-amd64", binaryContent)

	// Extract binary from archive.
	binaryPath, err := extractBinaryFromArchive(archivePath, "panvex-agent-linux-amd64", dir)
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

func TestExecute_RequestsPerArchAsset(t *testing.T) {
	var gotPaths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPaths = append(gotPaths, r.URL.Path)
		_, _ = w.Write([]byte("some-bytes"))
	}))
	t.Cleanup(srv.Close)

	cfg := defaultConfig()
	cfg.HTTPClient = srv.Client()
	cfg.AllowedHosts = []string{hostOf(t, srv.URL)}
	cfg.AllowInsecure = true

	_, err := executeWith(
		context.Background(),
		Payload{Version: "9.9.9", ReleaseBaseURL: srv.URL + "/download/agent/v9.9.9"},
		"1.0.0",
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		cfg,
	)
	// The flow now fetches the archive then the .sha256 sidecar. The served
	// sidecar ("some-bytes") is not the real SHA-256 of the served archive, so
	// verification must fail at the checksum step — which proves we got past URL
	// construction, the archive download, and the checksum download.
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("want checksum mismatch error, got %v", err)
	}
	wantArchive := "/download/agent/v9.9.9/panvex-agent-linux-" + runtime.GOARCH + ".tar.gz"
	if len(gotPaths) == 0 || gotPaths[0] != wantArchive {
		t.Fatalf("first request path = %v, want %q", gotPaths, wantArchive)
	}
	if len(gotPaths) < 2 || gotPaths[1] != wantArchive+".sha256" {
		t.Fatalf("second request path = %v, want %q", gotPaths, wantArchive+".sha256")
	}
}

func TestReplaceSelfReadOnlyDirFailsWithoutLosingCurrent(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission semantics require non-root")
	}
	dir := t.TempDir()
	current := filepath.Join(dir, "agent")
	next := filepath.Join(dir, "agent.next")
	for _, f := range []string{current, next} {
		if err := os.WriteFile(f, []byte(f), 0o700); err != nil { //nolint:gosec // G306: test fixtures stand in for executable binaries
			t.Fatal(err)
		}
	}
	if err := os.Chmod(dir, 0o555); err != nil { //nolint:gosec // G302: 0o555 (read-only, no write) is the point of this test
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) }) //nolint:gosec // G302: restores the temp dir so t.TempDir() cleanup can remove it

	err := replaceSelf(current, next)
	if err == nil {
		t.Fatal("want permission error, got nil")
	}
	if _, statErr := os.Stat(current); statErr != nil {
		t.Fatalf("current binary lost after failed swap: %v", statErr)
	}
}

func TestReplaceSelfRestoresCurrentWhenSwapFailsMidway(t *testing.T) {
	dir := t.TempDir()
	current := filepath.Join(dir, "agent")
	originalContent := []byte("original-binary-content")
	if err := os.WriteFile(current, originalContent, 0o700); err != nil { //nolint:gosec // G306: test fixture stands in for an executable binary
		t.Fatal(err)
	}
	// next deliberately does not exist: the first rename (current->backup)
	// succeeds, then the second rename (next->current) fails with ENOENT,
	// forcing replaceSelf down its restore path.
	next := filepath.Join(dir, "agent.next")

	err := replaceSelf(current, next)
	if err == nil {
		t.Fatal("want error from missing next binary, got nil")
	}
	got, statErr := os.Stat(current)
	if statErr != nil {
		t.Fatalf("current binary not restored after failed swap: %v", statErr)
	}
	if got.IsDir() {
		t.Fatalf("current is a directory, want the restored file")
	}
	restored, err := os.ReadFile(current)
	if err != nil {
		t.Fatalf("read restored current: %v", err)
	}
	if string(restored) != string(originalContent) {
		t.Fatalf("restored content = %q, want %q (original content)", restored, originalContent)
	}
}

func TestStageBinaryStagesNextToDestination(t *testing.T) {
	dir := t.TempDir()
	path, err := stageBinary(dir, strings.NewReader("fake-binary"))
	if err != nil {
		t.Fatalf("stageBinary: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(path) })
	if filepath.Dir(path) != dir {
		t.Fatalf("staged in %s, want %s", filepath.Dir(path), dir)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("perm = %v, want 0700", info.Mode().Perm())
	}
}

func TestParseChecksumSidecar(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"bare digest", "abc123", "abc123"},
		{"sha256sum format", "abc123  panvex-agent-linux-amd64.tar.gz", "abc123"},
		{"trailing newline", "abc123\n", "abc123"},
		{"leading whitespace", "  abc123  ", "abc123"},
		{"empty", "", ""},
		{"whitespace only", "   \n\t ", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseChecksumSidecar([]byte(tc.in)); got != tc.want {
				t.Fatalf("parseChecksumSidecar(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
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
