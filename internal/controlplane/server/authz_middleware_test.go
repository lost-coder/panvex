package server

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/auth"
	"github.com/lost-coder/panvex/internal/controlplane/storage/sqlite"
)

func TestRequireAuthenticatedSessionRejectsUnauthenticated(t *testing.T) {
	now := time.Date(2026, time.April, 15, 10, 0, 0, 0, time.UTC)
	server := New(Options{
		Now: func() time.Time { return now },
	})

	handler := server.requireAuthenticatedSession()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestRequireAuthenticatedSessionAllowsValidSession(t *testing.T) {
	now := time.Date(2026, time.April, 15, 10, 0, 0, 0, time.UTC)
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	srv := New(Options{
		Now:   func() time.Time { return now },
		Store: store,
	})
	defer srv.Close()

	if _, _, err := srv.auth.BootstrapUser(auth.BootstrapInput{
		Username: "admin",
		Password: "Admin1password",
		Role:     auth.RoleAdmin,
	}, now); err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}

	loginResp := performJSONRequest(t, srv.Handler(), http.MethodPost, "/api/auth/login", map[string]string{
		"username": "admin",
		"password": "Admin1password",
	}, nil)
	if loginResp.Code != http.StatusOK {
		t.Fatalf("login status = %d, want %d", loginResp.Code, http.StatusOK)
	}
	cookies := loginResp.Result().Cookies()

	// Hit /api/auth/me which uses requireAuthenticatedSession.
	meResp := performJSONRequest(t, srv.Handler(), http.MethodGet, "/api/auth/me", nil, cookies)
	if meResp.Code != http.StatusOK {
		t.Fatalf("GET /api/auth/me status = %d, want %d", meResp.Code, http.StatusOK)
	}
}

func TestRequireMinimumRoleAdminRejectsViewer(t *testing.T) {
	now := time.Date(2026, time.April, 15, 10, 0, 0, 0, time.UTC)
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	srv := New(Options{
		Now:   func() time.Time { return now },
		Store: store,
	})
	defer srv.Close()

	if _, _, err := srv.auth.BootstrapUser(auth.BootstrapInput{
		Username: "admin",
		Password: "Admin1password",
		Role:     auth.RoleAdmin,
	}, now); err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}

	// Create a viewer user.
	loginAdmin := performJSONRequest(t, srv.Handler(), http.MethodPost, "/api/auth/login", map[string]string{
		"username": "admin",
		"password": "Admin1password",
	}, nil)
	adminCookies := loginAdmin.Result().Cookies()

	createResp := performJSONRequest(t, srv.Handler(), http.MethodPost, "/api/users", map[string]string{
		"username": "viewer",
		"role":     "viewer",
		"password": "Viewer1password",
	}, adminCookies)
	if createResp.Code != http.StatusCreated {
		t.Fatalf("POST /api/users status = %d, want %d", createResp.Code, http.StatusCreated)
	}

	// Login as viewer.
	loginViewer := performJSONRequest(t, srv.Handler(), http.MethodPost, "/api/auth/login", map[string]string{
		"username": "viewer",
		"password": "Viewer1password",
	}, nil)
	viewerCookies := loginViewer.Result().Cookies()

	// Viewer should be rejected from admin-only endpoint (e.g. POST /api/users).
	createAsViewer := performJSONRequest(t, srv.Handler(), http.MethodPost, "/api/users", map[string]string{
		"username": "hacker",
		"role":     "admin",
		"password": "Hacker1password",
	}, viewerCookies)
	if createAsViewer.Code != http.StatusForbidden {
		t.Fatalf("POST /api/users as viewer status = %d, want %d", createAsViewer.Code, http.StatusForbidden)
	}
}

func TestRequireMinimumRoleAdminAllowsAdmin(t *testing.T) {
	now := time.Date(2026, time.April, 15, 10, 0, 0, 0, time.UTC)
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	srv := New(Options{
		Now:   func() time.Time { return now },
		Store: store,
	})
	defer srv.Close()

	if _, _, err := srv.auth.BootstrapUser(auth.BootstrapInput{
		Username: "admin",
		Password: "Admin1password",
		Role:     auth.RoleAdmin,
	}, now); err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}

	loginResp := performJSONRequest(t, srv.Handler(), http.MethodPost, "/api/auth/login", map[string]string{
		"username": "admin",
		"password": "Admin1password",
	}, nil)
	adminCookies := loginResp.Result().Cookies()

	// Admin should be able to list users (admin-only endpoint).
	listResp := performJSONRequest(t, srv.Handler(), http.MethodGet, "/api/users", nil, adminCookies)
	if listResp.Code != http.StatusOK {
		t.Fatalf("GET /api/users as admin status = %d, want %d", listResp.Code, http.StatusOK)
	}
}

func TestRoleSatisfiesHierarchy(t *testing.T) {
	tests := []struct {
		current  auth.Role
		required auth.Role
		want     bool
	}{
		{auth.RoleAdmin, auth.RoleAdmin, true},
		{auth.RoleAdmin, auth.RoleOperator, true},
		{auth.RoleAdmin, auth.RoleViewer, true},
		{auth.RoleOperator, auth.RoleAdmin, false},
		{auth.RoleOperator, auth.RoleOperator, true},
		{auth.RoleOperator, auth.RoleViewer, true},
		{auth.RoleViewer, auth.RoleAdmin, false},
		{auth.RoleViewer, auth.RoleOperator, false},
		{auth.RoleViewer, auth.RoleViewer, true},
	}

	for _, tt := range tests {
		got := roleSatisfies(tt.current, tt.required)
		if got != tt.want {
			t.Errorf("roleSatisfies(%q, %q) = %v, want %v", tt.current, tt.required, got, tt.want)
		}
	}
}

func TestForbiddenMessageForRole(t *testing.T) {
	msg := forbiddenMessageForRole(auth.RoleAdmin)
	if msg != "admin role required" {
		t.Fatalf("forbiddenMessageForRole(admin) = %q, want %q", msg, "admin role required")
	}

	msg = forbiddenMessageForRole(auth.RoleOperator)
	if msg == "" {
		t.Fatal("forbiddenMessageForRole(operator) = empty")
	}
}

func TestRequestAuthContextRoundTrip(t *testing.T) {
	session := auth.Session{ID: "sess-1", UserID: "user-1"}
	user := auth.User{ID: "user-1", Username: "alice", Role: auth.RoleAdmin}

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req = withRequestAuthContext(req, session, user)

	gotSession, gotUser, ok := requestAuthContext(req)
	if !ok {
		t.Fatal("requestAuthContext() ok = false, want true")
	}
	if gotSession.ID != session.ID {
		t.Fatalf("session.ID = %q, want %q", gotSession.ID, session.ID)
	}
	if gotUser.Username != user.Username {
		t.Fatalf("user.Username = %q, want %q", gotUser.Username, user.Username)
	}
}

func TestRequestAuthContextMissing(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/test", nil)

	_, _, ok := requestAuthContext(req)
	if ok {
		t.Fatal("requestAuthContext() ok = true on bare request, want false")
	}
}
