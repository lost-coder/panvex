package telemtupdate

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// hostOf parses an httptest server URL (http or https — NewTLSServer still
// prints an "https://" URL) and returns just the hostname (no port),
// matching what url.Hostname() returns and therefore the shape
// Config.AllowedHosts is checked against.
func hostOf(t *testing.T, rawURL string) string {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse test url %q: %v", rawURL, err)
	}
	return u.Hostname()
}

// ---- fetchToTemp ----

func TestFetchToTemp_HappyPath(t *testing.T) {
	body := []byte("fake-telemt-archive-bytes")
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(body)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)

	destDir := t.TempDir()
	cfg := Config{
		HTTPClient:   srv.Client(),
		AllowedHosts: []string{hostOf(t, srv.URL)},
		MaxArchive:   1024,
	}

	path, sum, err := fetchToTemp(context.Background(), cfg, srv.URL+"/telemt.tar.gz", destDir)
	if err != nil {
		t.Fatalf("fetchToTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(path) })

	if filepath.Dir(path) != destDir {
		t.Fatalf("temp file %q not staged in destDir %q", path, destDir)
	}
	if !strings.HasPrefix(filepath.Base(path), ".telemt-update-") {
		t.Fatalf("temp file name %q does not use the .telemt-update- prefix", filepath.Base(path))
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, body) {
		t.Fatalf("body = %q, want %q", got, body)
	}

	want := sha256.Sum256(body)
	if sum != hex.EncodeToString(want[:]) {
		t.Fatalf("sha256hex = %q, want %q", sum, hex.EncodeToString(want[:]))
	}
}

func TestFetchToTemp_RejectsHTTPScheme(t *testing.T) {
	cfg := Config{AllowedHosts: []string{"example.com"}}
	_, _, err := fetchToTemp(context.Background(), cfg, "http://example.com/telemt.tar.gz", t.TempDir())
	if !errors.Is(err, errInsecureScheme) {
		t.Fatalf("want errInsecureScheme, got %v", err)
	}
}

func TestFetchToTemp_RejectsHostNotInAllowlist(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("nope"))
	}))
	t.Cleanup(srv.Close)

	cfg := Config{
		HTTPClient:   srv.Client(),
		AllowedHosts: []string{"github.com"},
	}
	_, _, err := fetchToTemp(context.Background(), cfg, srv.URL+"/telemt.tar.gz", t.TempDir())
	if !errors.Is(err, errHostNotAllowed) {
		t.Fatalf("want errHostNotAllowed, got %v", err)
	}
}

func TestFetchToTemp_RejectsContentLengthOverLimit(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "9999999")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("tiny"))
	}))
	t.Cleanup(srv.Close)

	cfg := Config{
		HTTPClient:   srv.Client(),
		AllowedHosts: []string{hostOf(t, srv.URL)},
		MaxArchive:   1024,
	}
	_, _, err := fetchToTemp(context.Background(), cfg, srv.URL+"/telemt.tar.gz", t.TempDir())
	if !errors.Is(err, errArchiveTooLarge) {
		t.Fatalf("want errArchiveTooLarge, got %v", err)
	}
}

func TestFetchToTemp_RejectsBodyOverLimit(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// No Content-Length so pre-stream rejection cannot fire; the
		// LimitReader during copy must catch the overflow.
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(make([]byte, 8192))
	}))
	t.Cleanup(srv.Close)

	cfg := Config{
		HTTPClient:   srv.Client(),
		AllowedHosts: []string{hostOf(t, srv.URL)},
		MaxArchive:   1024,
	}
	_, _, err := fetchToTemp(context.Background(), cfg, srv.URL+"/telemt.tar.gz", t.TempDir())
	if !errors.Is(err, errArchiveTooLarge) {
		t.Fatalf("want errArchiveTooLarge, got %v", err)
	}
}

// ---- fetchChecksum ----

func TestFetchChecksum_HappyPath(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("abc123deadbeef  telemt.tar.gz\n"))
	}))
	t.Cleanup(srv.Close)

	cfg := Config{
		HTTPClient:   srv.Client(),
		AllowedHosts: []string{hostOf(t, srv.URL)},
	}
	got, err := fetchChecksum(context.Background(), cfg, srv.URL+"/telemt.tar.gz.sha256")
	if err != nil {
		t.Fatalf("fetchChecksum: %v", err)
	}
	if got != "abc123deadbeef" {
		t.Fatalf("checksum = %q, want %q", got, "abc123deadbeef")
	}
}

func TestFetchChecksum_HonoursMaxBytes(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", 10*1024)))
	}))
	t.Cleanup(srv.Close)

	cfg := Config{
		HTTPClient:   srv.Client(),
		AllowedHosts: []string{hostOf(t, srv.URL)},
	}
	got, err := fetchChecksum(context.Background(), cfg, srv.URL+"/telemt.tar.gz.sha256")
	if err != nil {
		t.Fatalf("fetchChecksum: %v", err)
	}
	if len(got) > defaultMaxChecksum {
		t.Fatalf("checksum bigger than cap: %d", len(got))
	}
}

// TestFetchToTemp_ChecksumMatchesSidecar exercises the full happy path an
// Execute-style caller will drive: download the archive while hashing it,
// separately fetch the published sidecar digest, and confirm the two
// agree. TestFetchToTemp_ChecksumMismatch below is the negative case.
func TestFetchToTemp_ChecksumMatchesSidecar(t *testing.T) {
	body := []byte("fake-telemt-archive-bytes")
	sum := sha256.Sum256(body)
	hexSum := hex.EncodeToString(sum[:])

	mux := http.NewServeMux()
	mux.HandleFunc("/telemt.tar.gz", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(body)
	})
	mux.HandleFunc("/telemt.tar.gz.sha256", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(hexSum))
	})
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)

	cfg := Config{
		HTTPClient:   srv.Client(),
		AllowedHosts: []string{hostOf(t, srv.URL)},
		MaxArchive:   1024,
	}

	path, gotHex, err := fetchToTemp(context.Background(), cfg, srv.URL+"/telemt.tar.gz", t.TempDir())
	if err != nil {
		t.Fatalf("fetchToTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(path) })

	wantHex, err := fetchChecksum(context.Background(), cfg, srv.URL+"/telemt.tar.gz.sha256")
	if err != nil {
		t.Fatalf("fetchChecksum: %v", err)
	}

	if !strings.EqualFold(gotHex, wantHex) {
		t.Fatalf("checksum mismatch: got %s, want %s", gotHex, wantHex)
	}
}

func TestFetchToTemp_ChecksumMismatch(t *testing.T) {
	body := []byte("fake-telemt-archive-bytes")

	mux := http.NewServeMux()
	mux.HandleFunc("/telemt.tar.gz", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(body)
	})
	mux.HandleFunc("/telemt.tar.gz.sha256", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("0000000000000000000000000000000000000000000000000000000000000000"))
	})
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)

	cfg := Config{
		HTTPClient:   srv.Client(),
		AllowedHosts: []string{hostOf(t, srv.URL)},
		MaxArchive:   1024,
	}

	path, gotHex, err := fetchToTemp(context.Background(), cfg, srv.URL+"/telemt.tar.gz", t.TempDir())
	if err != nil {
		t.Fatalf("fetchToTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(path) })

	wantHex, err := fetchChecksum(context.Background(), cfg, srv.URL+"/telemt.tar.gz.sha256")
	if err != nil {
		t.Fatalf("fetchChecksum: %v", err)
	}

	if strings.EqualFold(gotHex, wantHex) {
		t.Fatalf("expected checksum mismatch, both = %s", gotHex)
	}
}

// ---- extractBinary ----

type tarEntry struct {
	name     string
	body     []byte
	typeflag byte
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
		if err := tw.WriteHeader(&tar.Header{
			Name:     e.name,
			Mode:     0o755,
			Size:     int64(len(e.body)),
			Typeflag: typeflag,
		}); err != nil {
			t.Fatalf("write header %s: %v", e.name, err)
		}
		if _, err := tw.Write(e.body); err != nil {
			t.Fatalf("write body %s: %v", e.name, err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar writer: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("close gzip writer: %v", err)
	}

	path := filepath.Join(t.TempDir(), "telemt.tar.gz")
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatalf("write archive: %v", err)
	}
	return path
}

func TestExtractBinary_SingleMember(t *testing.T) {
	binary := []byte("#!/bin/true\nfake-telemt-binary-payload")
	archive := makeTarGz(t, []tarEntry{
		{name: "telemt", body: binary},
	})

	destDir := t.TempDir()
	path, size, err := extractBinary(archive, destDir)
	if err != nil {
		t.Fatalf("extractBinary: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(path) })

	if filepath.Dir(path) != destDir {
		t.Fatalf("extracted binary %q not staged in destDir %q", path, destDir)
	}
	if size != int64(len(binary)) {
		t.Fatalf("size = %d, want %d", size, len(binary))
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read extracted: %v", err)
	}
	if !bytes.Equal(got, binary) {
		t.Fatalf("extracted %d bytes != expected %d bytes", len(got), len(binary))
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("perm = %o, want 0755", info.Mode().Perm())
	}
}

// TestExtractBinary_IgnoresMemberName confirms the extracted member's name
// is never trusted for the destination path — the classic tar
// path-traversal vector ("../../etc/passwd") is neutralised simply by
// always writing to our own os.CreateTemp file in destDir.
func TestExtractBinary_IgnoresMemberName(t *testing.T) {
	binary := []byte("payload")
	archive := makeTarGz(t, []tarEntry{
		{name: "../../../etc/telemt-evil", body: binary},
	})

	destDir := t.TempDir()
	path, _, err := extractBinary(archive, destDir)
	if err != nil {
		t.Fatalf("extractBinary: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(path) })

	if filepath.Dir(path) != destDir {
		t.Fatalf("extracted binary escaped destDir: %q", path)
	}
	if filepath.Base(path) == "telemt-evil" {
		t.Fatalf("member name leaked into extracted file name: %q", path)
	}
}

func TestExtractBinary_RejectsTwoMembers(t *testing.T) {
	archive := makeTarGz(t, []tarEntry{
		{name: "telemt", body: []byte("one")},
		{name: "telemt.old", body: []byte("two")},
	})

	_, _, err := extractBinary(archive, t.TempDir())
	if err == nil {
		t.Fatal("expected error for archive with two file members")
	}
}

// TestExtractBinary_AnySingleMemberNameAccepted confirms the member name is
// genuinely not checked: a single regular member named anything other than
// "telemt" (e.g. a stray README) still extracts successfully.
func TestExtractBinary_AnySingleMemberNameAccepted(t *testing.T) {
	archive := makeTarGz(t, []tarEntry{
		{name: "README.md", body: []byte("docs"), typeflag: tar.TypeReg},
	})
	path, _, err := extractBinary(archive, t.TempDir())
	if err != nil {
		t.Fatalf("extractBinary should ignore member name: %v", err)
	}
	_ = os.Remove(path)
}

func TestExtractBinary_RejectsNoRegularMembers(t *testing.T) {
	archive := makeTarGz(t, []tarEntry{
		{name: "empty-dir/", typeflag: tar.TypeDir},
	})
	_, _, err := extractBinary(archive, t.TempDir())
	if err == nil {
		t.Fatal("expected error for archive with no regular file member")
	}
}

func TestExtractBinary_RejectsOversizedMember(t *testing.T) {
	// A regular member's header declaring a size above the archive cap
	// must be rejected before any bytes are streamed for THAT member —
	// the fast pre-check on the entry we actually intend to extract.
	// (The general decompression-bomb defense covering every member,
	// including ones the loop never inspects the size of, is
	// TestExtractBinaryCapped_BlocksBombBehindNonRegularMember below.)
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	body := []byte("short")
	if err := tw.WriteHeader(&tar.Header{
		Name:     "telemt",
		Mode:     0o755,
		Size:     defaultMaxArchive + 1,
		Typeflag: tar.TypeReg,
	}); err != nil {
		t.Fatalf("write header: %v", err)
	}
	_, _ = tw.Write(body)
	_ = tw.Close()
	_ = gz.Close()

	path := filepath.Join(t.TempDir(), "telemt.tar.gz")
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatalf("write archive: %v", err)
	}

	_, _, err := extractBinary(path, t.TempDir())
	if err == nil {
		t.Fatal("expected error for oversized declared member size")
	}
}

// TestExtractBinaryCapped_BlocksBombBehindNonRegularMember is the
// regression test for the review finding: extractBinary's loop skips
// non-regular members (`continue`) without ever looking at their declared
// Size, but tar.Reader.Next still has to consume — i.e. decompress — the
// remaining bytes of a skipped entry to reach the next header. A hostile
// release host can sign a "telemt.tar.gz" whose sidecar checksum matches
// the archive bytes on disk perfectly (checksum verification never looks
// inside the archive) while burying a forged non-regular member (a
// symlink or directory header lying about a huge Size; '9' — an
// unrecognized/vendor typeflag — is used here purely because Go's
// archive/tar.Writer refuses to let a test *construct* an oversized
// Dir/Symlink/Link body, not because the vulnerable code path cares about
// the flag) ahead of the real "telemt" member. Without a cumulative cap on
// the decompressed stream, that bomb's declared size is paid for in
// CPU/wall-clock decompression time regardless of it never being
// extracted to disk.
//
// The test uses extractBinaryCapped directly with a tiny cap so it stays
// fast — no multi-gigabyte fixture required to prove the defense works;
// production always goes through extractBinary, which calls this with
// defaultMaxArchive (128 MiB).
func TestExtractBinaryCapped_BlocksBombBehindNonRegularMember(t *testing.T) {
	const smallCap = 64
	bomb := bytes.Repeat([]byte("B"), smallCap*4) // real bytes, well over smallCap
	archive := makeTarGz(t, []tarEntry{
		{name: "bomb", body: bomb, typeflag: '9'},
		{name: "telemt", body: []byte("real-binary-payload")},
	})

	_, _, err := extractBinaryCapped(archive, t.TempDir(), smallCap)
	if !errors.Is(err, errArchiveTooLarge) {
		t.Fatalf("want errArchiveTooLarge, got %v", err)
	}
}

// TestExtractBinaryCapped_AllowsArchiveWithinCap is the paired positive
// case: a normal single-member archive comfortably inside the cap must
// still extract successfully — the fix must not have turned the cap into
// a rejection of legitimate archives. The cap here is generous relative
// to the member's body (unlike production's 128 MiB vs. a few-MB Telemt
// binary, a tiny test archive's fixed per-entry tar overhead — the
// 512-byte header block, end-of-archive padding — is non-negligible
// relative to a few bytes of payload, so this deliberately does not probe
// an exact byte boundary; capReader bounds the whole decompressed stream,
// headers included, by design).
func TestExtractBinaryCapped_AllowsArchiveWithinCap(t *testing.T) {
	const smallCap = 4096
	binary := []byte("real-binary-payload")
	archive := makeTarGz(t, []tarEntry{
		{name: "telemt", body: binary},
	})

	path, size, err := extractBinaryCapped(archive, t.TempDir(), smallCap)
	if err != nil {
		t.Fatalf("extractBinaryCapped: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(path) })
	if size != int64(len(binary)) {
		t.Fatalf("size = %d, want %d", size, len(binary))
	}
}

// ---- checkDowngrade ----

func TestCheckDowngrade(t *testing.T) {
	tests := []struct {
		name          string
		current       string
		target        string
		allow         bool
		wantErr       bool
		wantAlreadyAt bool
	}{
		{
			name:          "equal versions is already-at-target",
			current:       "1.4.7",
			target:        "1.4.7",
			wantErr:       true,
			wantAlreadyAt: true,
		},
		{
			name:          "leading-v form compares equal",
			current:       "v1.4.7",
			target:        "1.4.7",
			wantErr:       true,
			wantAlreadyAt: true,
		},
		{
			name:    "older target refused",
			current: "1.4.7",
			target:  "1.4.6",
			wantErr: true,
		},
		{
			name:    "older major refused",
			current: "3.0.0",
			target:  "2.99.99",
			wantErr: true,
		},
		{
			name:    "newer target allowed",
			current: "1.4.7",
			target:  "1.5.0",
			wantErr: false,
		},
		{
			name:    "older target with allow_downgrade passes",
			current: "1.4.7",
			target:  "1.4.6",
			allow:   true,
			wantErr: false,
		},
		{
			name:    "equal target with allow_downgrade passes (forced reinstall)",
			current: "1.4.7",
			target:  "1.4.7",
			allow:   true,
			wantErr: false,
		},
		{
			name:    "current dev build refused without allow",
			current: "dev",
			target:  "1.0.0",
			wantErr: true,
		},
		{
			name:    "current dev build passes with allow",
			current: "dev",
			target:  "1.0.0",
			allow:   true,
			wantErr: false,
		},
		{
			name:    "unparseable target refused",
			current: "1.4.7",
			target:  "not-a-version",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkDowngrade(tt.current, tt.target, tt.allow)
			if (err != nil) != tt.wantErr {
				t.Fatalf("checkDowngrade(%q, %q, %v) error = %v, wantErr %v", tt.current, tt.target, tt.allow, err, tt.wantErr)
			}
			if tt.wantAlreadyAt && !errors.Is(err, errAlreadyAtTarget) {
				t.Fatalf("checkDowngrade(%q, %q, %v) = %v, want errAlreadyAtTarget", tt.current, tt.target, tt.allow, err)
			}
		})
	}
}

// ---- defaultConfig / redirectPolicy ----

func TestDefaultConfig_UsesArchiveCap(t *testing.T) {
	cfg := defaultConfig()
	if cfg.MaxArchive != defaultMaxArchive {
		t.Fatalf("MaxArchive = %d, want %d", cfg.MaxArchive, defaultMaxArchive)
	}
	if defaultMaxArchive != 128<<20 {
		t.Fatalf("defaultMaxArchive = %d, want 128 MiB", defaultMaxArchive)
	}
}

func TestRedirectPolicyRevalidatesHost(t *testing.T) {
	mk := func(u string) *http.Request {
		r, err := http.NewRequestWithContext(context.Background(), http.MethodGet, u, nil)
		if err != nil {
			t.Fatalf("build request: %v", err)
		}
		return r
	}
	strict := redirectPolicy(Config{AllowedHosts: []string{"github.com"}})
	if err := strict(mk("https://github.com/x"), nil); err != nil {
		t.Fatalf("allowed-host redirect rejected: %v", err)
	}
	if err := strict(mk("https://evil.example.com/x"), nil); err == nil {
		t.Fatal("redirect to off-list host accepted")
	}
	if err := strict(mk("http://github.com/x"), nil); err == nil {
		t.Fatal("redirect to http accepted")
	}
}
