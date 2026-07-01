package updater

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// hostOf parses an httptest server URL and returns just the hostname
// (no port), matching what url.Hostname() returns and therefore the
// shape Config.AllowedHosts is checked against.
func hostOf(t *testing.T, rawURL string) string {
	t.Helper()
	const prefix = "http://"
	if !strings.HasPrefix(rawURL, prefix) {
		t.Fatalf("unexpected test URL %q", rawURL)
	}
	hostPort := strings.TrimPrefix(rawURL, prefix)
	if i := strings.IndexByte(hostPort, ':'); i >= 0 {
		return hostPort[:i]
	}
	return hostPort
}

func TestValidateDownloadURL_RejectsNonHTTPS(t *testing.T) {
	cfg := Config{AllowedHosts: []string{"example.com"}}
	if err := validateDownloadURL("http://example.com/file", cfg); !errors.Is(err, errInsecureScheme) {
		t.Fatalf("want errInsecureScheme, got %v", err)
	}
}

func TestValidateDownloadURL_AllowsHTTPWhenOptedIn(t *testing.T) {
	cfg := Config{AllowedHosts: []string{"example.com"}, AllowInsecure: true}
	if err := validateDownloadURL("http://example.com/file", cfg); err != nil {
		t.Fatalf("opted-in http should be allowed, got %v", err)
	}
}

func TestValidateDownloadURL_RejectsHostNotInAllowlist(t *testing.T) {
	cfg := Config{AllowedHosts: []string{"github.com"}}
	if err := validateDownloadURL("https://attacker.example/file", cfg); !errors.Is(err, errHostNotAllowed) {
		t.Fatalf("want errHostNotAllowed, got %v", err)
	}
}

func TestValidateDownloadURL_AcceptsAllowedHost(t *testing.T) {
	cfg := Config{AllowedHosts: []string{"github.com"}}
	if err := validateDownloadURL("https://github.com/owner/repo/releases/download/v1/file", cfg); err != nil {
		t.Fatalf("allowed host should accept, got %v", err)
	}
}

func TestValidateDownloadURL_RejectsEmpty(t *testing.T) {
	cfg := Config{AllowedHosts: []string{"github.com"}}
	if err := validateDownloadURL("", cfg); err == nil {
		t.Fatalf("expected error for empty url")
	}
}

func TestDownloadToTemp_RejectsContentLengthOverLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Lie about Content-Length to trigger pre-stream rejection.
		w.Header().Set("Content-Length", "9999999")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("tiny"))
	}))
	t.Cleanup(srv.Close)

	cfg := Config{
		HTTPClient:    srv.Client(),
		AllowedHosts:  []string{hostOf(t, srv.URL)},
		AllowInsecure: true,
		MaxArchive:    1024,
	}
	_, err := downloadToTemp(context.Background(), srv.URL+"/file", cfg)
	if !errors.Is(err, errArchiveTooLarge) {
		t.Fatalf("want errArchiveTooLarge, got %v", err)
	}
}

func TestDownloadToTemp_RejectsBodyOverLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// No Content-Length so we cannot reject early; the LimitReader
		// must catch the overflow during streaming.
		w.Header().Set("Content-Type", "application/octet-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(make([]byte, 8192))
	}))
	t.Cleanup(srv.Close)

	cfg := Config{
		HTTPClient:    srv.Client(),
		AllowedHosts:  []string{hostOf(t, srv.URL)},
		AllowInsecure: true,
		MaxArchive:    1024,
	}
	_, err := downloadToTemp(context.Background(), srv.URL+"/file", cfg)
	if !errors.Is(err, errArchiveTooLarge) {
		t.Fatalf("want errArchiveTooLarge, got %v", err)
	}
}

func TestDownloadToTemp_HappyPath(t *testing.T) {
	body := []byte("hello-archive-bytes")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(body)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)

	cfg := Config{
		HTTPClient:    srv.Client(),
		AllowedHosts:  []string{hostOf(t, srv.URL)},
		AllowInsecure: true,
		MaxArchive:    1024,
	}
	path, err := downloadToTemp(context.Background(), srv.URL+"/file", cfg)
	if err != nil {
		t.Fatalf("downloadToTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(path) })

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(body) {
		t.Fatalf("body = %q, want %q", got, body)
	}
}

func TestDownloadToTemp_TimesOutOnSlowServer(t *testing.T) {
	// Server holds the response until the request context is cancelled.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
		w.WriteHeader(http.StatusGatewayTimeout)
	}))
	t.Cleanup(srv.Close)

	client := &http.Client{Transport: srv.Client().Transport, Timeout: 100 * time.Millisecond}
	cfg := Config{
		HTTPClient:    client,
		AllowedHosts:  []string{hostOf(t, srv.URL)},
		AllowInsecure: true,
		MaxArchive:    1024,
	}
	if _, err := downloadToTemp(context.Background(), srv.URL+"/slow", cfg); err == nil {
		t.Fatalf("expected timeout error, got nil")
	}
}

func TestDownloadToTemp_RejectsDisallowedHost(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("nope"))
	}))
	t.Cleanup(srv.Close)

	cfg := Config{
		HTTPClient:    srv.Client(),
		AllowedHosts:  []string{"github.com"},
		AllowInsecure: true,
		MaxArchive:    1024,
	}
	if _, err := downloadToTemp(context.Background(), srv.URL+"/file", cfg); !errors.Is(err, errHostNotAllowed) {
		t.Fatalf("want errHostNotAllowed, got %v", err)
	}
}

func TestDownloadBytes_HonoursMaxBytes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Stream 10KB into a 1KB cap.
		_, _ = io.Copy(w, io.LimitReader(strings.NewReader(strings.Repeat("x", 10*1024)), 10*1024))
	}))
	t.Cleanup(srv.Close)

	cfg := Config{
		HTTPClient:    srv.Client(),
		AllowedHosts:  []string{hostOf(t, srv.URL)},
		AllowInsecure: true,
	}
	body, err := downloadBytes(context.Background(), srv.URL+"/sig", 1024, cfg)
	if err != nil {
		t.Fatalf("downloadBytes: %v", err)
	}
	if len(body) > 1024 {
		t.Fatalf("body bigger than cap: %d", len(body))
	}
}

func TestValidateDownloadURLWildcardAllowsAnyHost(t *testing.T) {
	t.Setenv(EnvAllowedHosts, "*")
	cfg := defaultConfig()
	if err := validateDownloadURL("https://my-mirror.internal/agent.tar.gz", cfg); err != nil {
		t.Fatalf("wildcard config rejected an https mirror host: %v", err)
	}
	if err := validateDownloadURL("http://my-mirror.internal/agent.tar.gz", cfg); err == nil {
		t.Fatal("wildcard config accepted http — https must still be enforced")
	}
}

func TestValidateDownloadURLDefaultRejectsOffList(t *testing.T) {
	t.Setenv(EnvAllowedHosts, "")
	cfg := defaultConfig()
	if err := validateDownloadURL("https://evil.example.com/agent.tar.gz", cfg); err == nil {
		t.Fatal("default config accepted an off-list host")
	}
}

func TestRedirectPolicyRevalidatesHost(t *testing.T) {
	mk := func(u string) *http.Request {
		r, err := http.NewRequestWithContext(t.Context(), http.MethodGet, u, nil)
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
	wild := redirectPolicy(Config{AllowAnyHost: true})
	if err := wild(mk("https://anything.example.com/x"), nil); err != nil {
		t.Fatalf("wildcard redirect rejected an https host: %v", err)
	}
	if err := wild(mk("http://anything.example.com/x"), nil); err == nil {
		t.Fatal("wildcard redirect accepted http")
	}
}
