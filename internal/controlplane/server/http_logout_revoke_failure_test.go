package server

import (
	"context"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/auth"
	"github.com/lost-coder/panvex/internal/controlplane/storage/sqlite"
)

// TestHTTPLogoutReturns503WhenSessionStoreDeleteFails verifies the HTTP
// surface of the Logout fail-closed fix (PVX-002, follow-up to V2.1/V2.2):
// a store outage during logout is not "unauthorized" — the session is still
// valid (Logout leaves it in memory on a genuine store failure, D1) — so the
// handler must answer 503 (retry-able), never 401, and must not clear the
// session cookie on this path (the client should retry the same logout).
func TestHTTPLogoutReturns503WhenSessionStoreDeleteFails(t *testing.T) {
	now := time.Date(2026, time.July, 17, 16, 0, 0, 0, time.UTC)
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	server := mustNew(t, Options{
		LoginTimingFloor: -1,
		Now:              func() time.Time { return now },
		Store:            store,
	})
	defer server.Close()

	if _, _, err := server.auth.BootstrapUser(context.Background(), auth.BootstrapInput{
		Username: "operator",
		Password: "Operator1password",
		Role:     auth.RoleOperator,
	}, now); err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}

	loginResp := performJSONRequest(t, server, http.MethodPost, "/api/auth/login", map[string]string{
		"username": "operator",
		"password": "Operator1password",
	}, nil)
	if loginResp.Code != http.StatusOK {
		t.Fatalf("login status = %d, want %d", loginResp.Code, http.StatusOK)
	}
	cookies := loginResp.Result().Cookies()

	// Swap in a store that refuses every DeleteSession from here on, then
	// attempt to log out. The session was real and is still valid — this
	// must not be conflated with "no session" (401).
	server.auth.SetSessionStore(alwaysFailSessionStore{})

	logoutResp := performJSONRequest(t, server, http.MethodPost, "/api/auth/logout", nil, cookies)
	if logoutResp.Code != http.StatusServiceUnavailable {
		t.Fatalf("logout status = %d, want %d (store outage must be retry-able, not unauthorized)", logoutResp.Code, http.StatusServiceUnavailable)
	}

	// The session must still be valid — Logout kept it in memory on the
	// failed store delete, so /me should still succeed with the same
	// cookies, and the client can retry the logout.
	meResp := performJSONRequest(t, server, http.MethodGet, "/api/auth/me", nil, cookies)
	if meResp.Code != http.StatusOK {
		t.Fatalf("GET /api/auth/me after failed logout status = %d, want %d (session must survive a failed logout)", meResp.Code, http.StatusOK)
	}
}

// TestHTTPLogoutReturns401ForUnknownSession is the control for the test
// above: an unknown/missing session must still map to 401, not 503 — the
// two failure modes (no session vs. store outage on a real session) must
// stay distinguishable at the HTTP layer.
func TestHTTPLogoutReturns401ForUnknownSession(t *testing.T) {
	now := time.Date(2026, time.July, 17, 16, 5, 0, 0, time.UTC)
	server := mustNew(t, Options{
		LoginTimingFloor: -1,
		Now:              func() time.Time { return now },
	})
	defer server.Close()

	resp := performJSONRequest(t, server, http.MethodPost, "/api/auth/logout", nil, nil)
	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("logout without session status = %d, want %d", resp.Code, http.StatusUnauthorized)
	}
}
