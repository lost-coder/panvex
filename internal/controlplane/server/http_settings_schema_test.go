package server

import (
	"compress/gzip"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/auth"
	"github.com/lost-coder/panvex/internal/controlplane/storage/sqlite"
)

// newAuthedServer returns a Server wired to a fresh SQLite store, a
// logged-in viewer session cookie slice, and the underlying store.
// It is used by T22+ handler tests that need an authenticated context.
func newAuthedServer(t *testing.T) (*Server, *sqlite.Store, []*http.Cookie) {
	t.Helper()
	now := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	server := mustNew(t, Options{
		LoginTimingFloor: -1,
		Now:              func() time.Time { return now },
		Store:            store,
	})
	t.Cleanup(func() {
		server.Close()
		store.Close()
	})

	if _, _, err := server.auth.BootstrapUser(context.Background(), auth.BootstrapInput{
		Username: "admin",
		Password: "Admin1password",
		Role:     auth.RoleAdmin,
	}, now); err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}

	loginResp := performJSONRequest(t, server, http.MethodPost, "/api/auth/login", map[string]string{
		"username": "admin",
		"password": "Admin1password",
	}, nil)
	if loginResp.Code != http.StatusOK {
		t.Fatalf("login status = %d", loginResp.Code)
	}
	return server, store, loginResp.Result().Cookies()
}

func TestHTTPSettingsSchema_ReturnsRegistry(t *testing.T) {
	server, _, cookies := newAuthedServer(t)
	resp := performJSONRequest(t, server, http.MethodGet, "/api/settings/schema", nil, cookies)
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d", resp.Code)
	}
	body := resp.Body.String()
	for _, want := range []string{
		`"http.listen_address"`,
		`"auth.password_min_length"`,
		`"class": "bootstrap"`,
		`"class": "operational"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in body", want)
		}
	}
}

func TestHTTPSettingsSchema_ReturnsETag(t *testing.T) {
	server, _, cookies := newAuthedServer(t)
	resp := performJSONRequest(t, server, http.MethodGet, "/api/settings/schema", nil, cookies)
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d", resp.Code)
	}
	etag := resp.Header().Get("ETag")
	if etag == "" || !strings.HasPrefix(etag, `"`) || !strings.HasSuffix(etag, `"`) {
		t.Fatalf("ETag malformed: %q", etag)
	}
}

func TestHTTPSettingsSchema_HonoursIfNoneMatch(t *testing.T) {
	server, _, cookies := newAuthedServer(t)
	first := performJSONRequest(t, server, http.MethodGet, "/api/settings/schema", nil, cookies)
	etag := first.Header().Get("ETag")
	if etag == "" {
		t.Fatal("expected ETag on first request")
	}
	// second request with If-None-Match should return 304
	req := httptest.NewRequest(http.MethodGet, "/api/settings/schema", nil)
	req.Header.Set("If-None-Match", etag)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rr := httptest.NewRecorder()
	server.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusNotModified {
		t.Fatalf("status = %d, want 304", rr.Code)
	}
	if rr.Header().Get("ETag") != etag {
		t.Errorf("304 must echo ETag")
	}
}

func TestHTTPSettingsSchema_GzipCompresses(t *testing.T) {
	server, _, cookies := newAuthedServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/settings/schema", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rr := httptest.NewRecorder()
	server.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
	if rr.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("Content-Encoding = %q", rr.Header().Get("Content-Encoding"))
	}
	gz, err := gzip.NewReader(rr.Body)
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	body, err := io.ReadAll(gz)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"http.listen_address"`) {
		t.Errorf("decompressed body missing schema content")
	}
}
