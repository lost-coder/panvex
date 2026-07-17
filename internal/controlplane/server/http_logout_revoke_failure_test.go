package server

import (
	"context"
	"net/http"
	"net/http/httptest"
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

	// The 503 response must NOT clear the session cookie: the session is
	// still valid (kept in memory, D1) and the client should retry the same
	// logout with the same cookie, not treat itself as already logged out.
	for _, c := range logoutResp.Result().Cookies() {
		if (c.Name == sessionCookieName || c.Name == sessionCookieNameHostPrefix) && c.MaxAge < 0 {
			t.Fatalf("logout 503 response cleared cookie %q (MaxAge=%d), want untouched — the session is still valid and the client must retry with it", c.Name, c.MaxAge)
		}
	}

	// The session must still be valid — Logout kept it in memory on the
	// failed store delete, so /me should still succeed with the same
	// cookies, and the client can retry the logout.
	meResp := performJSONRequest(t, server, http.MethodGet, "/api/auth/me", nil, cookies)
	if meResp.Code != http.StatusOK {
		t.Fatalf("GET /api/auth/me after failed logout status = %d, want %d (session must survive a failed logout)", meResp.Code, http.StatusOK)
	}
}

// TestHTTPLogoutReturns401WhenSessionAlreadyGoneAtLogoutCall reaches the
// specific errors.Is(err, auth.ErrSessionNotFound) branch inside
// handleLogout's error path, which the outer "no cookie at all" test
// (TestHTTPLogoutReturns401ForUnknownSession) never touches — that one
// trips requireSession's own 401 before handleLogout ever calls
// s.auth.Logout. /api/auth/logout runs behind requireAuthenticatedSession
// middleware, which resolves the session ONCE and caches it in the request
// context; requireSession inside the handler then reads that cache instead
// of re-querying the live map (authz_middleware.go: requestAuthContext).
// So we manufacture the exact "requireSession still has it cached, but the
// live session is already gone" state deterministically: authenticate for
// real, revoke that same session out from under it via a direct auth.Logout
// call (simulating a concurrent second tab's logout, or the cleanup worker
// winning a race), then invoke the handler directly with a request whose
// context already carries the now-stale session — exactly what the
// middleware would have handed it had the race gone the other way.
func TestHTTPLogoutReturns401WhenSessionAlreadyGoneAtLogoutCall(t *testing.T) {
	now := time.Date(2026, time.July, 17, 16, 10, 0, 0, time.UTC)
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

	session, err := server.auth.Authenticate(context.Background(), auth.LoginInput{
		Username: "operator",
		Password: "Operator1password",
	}, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
	user, err := server.auth.GetUserByID(context.Background(), session.UserID)
	if err != nil {
		t.Fatalf("GetUserByID() error = %v", err)
	}

	// The session is genuinely revoked already — as if a concurrent request
	// (another tab, or the cleanup worker) won the race and logged it out
	// first. This is a real, successful Logout on a healthy store.
	if err := server.auth.Logout(context.Background(), session.ID); err != nil {
		t.Fatalf("Logout() (setup) error = %v, want nil", err)
	}

	// Invoke handleLogout directly with a request whose context already
	// carries the now-stale session, bypassing the middleware chain (which
	// would otherwise re-resolve from the live map and fail earlier, at the
	// OUTER 401 check this test is not targeting).
	req := withRequestAuthContext(httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/auth/logout", nil), session, user)
	w := httptest.NewRecorder()
	server.handleLogout()(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("handleLogout() with already-revoked session status = %d, want %d (errors.Is(..., ErrSessionNotFound) branch)", w.Code, http.StatusUnauthorized)
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
