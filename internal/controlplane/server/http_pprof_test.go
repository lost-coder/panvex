package server

import (
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/auth"
	"github.com/lost-coder/panvex/internal/controlplane/storage/sqlite"
)

// TestPprofAdminOnly verifies that /api/debug/pprof/* is gated behind the
// admin role guard. Viewers and operators must receive 403; admins must be
// able to fetch the index and the goroutine named profile without error.
//
// P3-OBS-02.
func TestPprofAdminOnly(t *testing.T) {
	now := time.Date(2026, time.April, 18, 12, 0, 0, 0, time.UTC)
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	server := New(Options{
		Now:   func() time.Time { return now },
		Store: store,
	})
	defer server.Close()

	if _, _, err := server.auth.BootstrapUser(auth.BootstrapInput{
		Username: "admin",
		Password: "Admin1password",
		Role:     auth.RoleAdmin,
	}, now); err != nil {
		t.Fatalf("BootstrapUser(admin) error = %v", err)
	}

	adminLogin := performJSONRequest(t, server.Handler(), http.MethodPost, "/api/auth/login", map[string]string{
		"username": "admin",
		"password": "Admin1password",
	}, nil)
	if adminLogin.Code != http.StatusOK {
		t.Fatalf("admin login status = %d, want %d", adminLogin.Code, http.StatusOK)
	}
	adminCookies := adminLogin.Result().Cookies()

	// Admin: /api/debug/pprof/ (index)
	indexResponse := performJSONRequest(t, server.Handler(), http.MethodGet, "/api/debug/pprof/", nil, adminCookies)
	if indexResponse.Code != http.StatusOK {
		t.Fatalf("admin GET /api/debug/pprof/ status = %d, want %d (body=%q)", indexResponse.Code, http.StatusOK, indexResponse.Body.String())
	}

	// Admin: /api/debug/pprof/goroutine (named profile). debug=1 returns text
	// so the test does not need to decode the binary proto format.
	goroutineResponse := performJSONRequest(t, server.Handler(), http.MethodGet, "/api/debug/pprof/goroutine?debug=1", nil, adminCookies)
	if goroutineResponse.Code != http.StatusOK {
		t.Fatalf("admin GET /api/debug/pprof/goroutine status = %d, want %d", goroutineResponse.Code, http.StatusOK)
	}

	// Viewer account — must receive 403.
	if _, err := server.auth.CreateUser(auth.BootstrapInput{
		Username: "viewer",
		Password: "Viewer1password",
		Role:     auth.RoleViewer,
	}, now); err != nil {
		t.Fatalf("CreateUser(viewer) error = %v", err)
	}
	viewerLogin := performJSONRequest(t, server.Handler(), http.MethodPost, "/api/auth/login", map[string]string{
		"username": "viewer",
		"password": "Viewer1password",
	}, nil)
	if viewerLogin.Code != http.StatusOK {
		t.Fatalf("viewer login status = %d, want %d", viewerLogin.Code, http.StatusOK)
	}
	viewerCookies := viewerLogin.Result().Cookies()

	viewerResponse := performJSONRequest(t, server.Handler(), http.MethodGet, "/api/debug/pprof/", nil, viewerCookies)
	if viewerResponse.Code != http.StatusForbidden {
		t.Fatalf("viewer GET /api/debug/pprof/ status = %d, want %d", viewerResponse.Code, http.StatusForbidden)
	}

	// Unauthenticated — no cookies, must receive 401.
	anonResponse := performJSONRequest(t, server.Handler(), http.MethodGet, "/api/debug/pprof/", nil, nil)
	if anonResponse.Code != http.StatusUnauthorized {
		t.Fatalf("anon GET /api/debug/pprof/ status = %d, want %d", anonResponse.Code, http.StatusUnauthorized)
	}
}
