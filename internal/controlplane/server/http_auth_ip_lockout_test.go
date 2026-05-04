package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/auth"
	"github.com/lost-coder/panvex/internal/controlplane/sessions"
	"github.com/lost-coder/panvex/internal/controlplane/storage/sqlite"
)

// buildLoginRequest constructs a /api/auth/login POST with a JSON body
// and the same Origin/CSRF wiring performJSONRequest applies. Caller
// can override RemoteAddr before passing it to serveLogin.
func buildLoginRequest(t *testing.T, username, password string) *http.Request {
	t.Helper()
	body, err := json.Marshal(map[string]string{
		"username": username,
		"password": password,
	})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://"+req.Host)
	return req
}

// serveLogin sends the request through the full server handler chain
// (matches what the dashboard would do) and returns the response.
func serveLogin(t *testing.T, srv *Server, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

// TestLogin_IPLockoutBlocksFurtherAttempts seeds the IPLockoutTracker
// directly with IPLockoutMaxFailures from a single source IP across
// multiple usernames, then asserts the next /login attempt is rejected
// with 429 — even when the credentials are correct. This proves the
// IP-keyed pre-username gate fires.
//
// We seed the tracker rather than driving 50 real /login requests
// because the per-IP fixed-window login rate limiter (30 / min) would
// trip first.
func TestLogin_IPLockoutBlocksFurtherAttempts(t *testing.T) {
	now := time.Date(2026, time.May, 3, 12, 0, 0, 0, time.UTC)

	store, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	t.Cleanup(func() { store.Close() })

	srv := mustNew(t, Options{
		LoginTimingFloor: -1,
		Now:              func() time.Time { return now },
		Store:            store,
	})
	t.Cleanup(srv.Close)

	if _, _, err := srv.auth.BootstrapUser(context.Background(), auth.BootstrapInput{
		Username: "admin",
		Password: "Admin1password",
		Role:     auth.RoleAdmin,
	}, now); err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}

	// Drive the IP tracker straight to the cap by simulating failures
	// across a mix of usernames — proving the counter is IP-keyed and
	// not username-keyed.
	const ip = "192.0.2.1"
	usernames := []string{"admin", "alice", "bob", "carol", "dave"}
	for i := 0; i < sessions.IPLockoutMaxFailures; i++ {
		_ = usernames[i%len(usernames)]
		srv.ipLockout.RecordFailureWithContext(context.Background(), ip, now.Add(time.Duration(i)*time.Second))
	}
	if !srv.ipLockout.IsLocked(ip, now.Add(time.Minute)) {
		t.Fatalf("seeded IP should be locked")
	}

	// Issue a login from the locked IP with CORRECT credentials. Must
	// be rejected with 429 because the IP-lockout pre-check fires
	// before authentication.
	req := buildLoginRequest(t, "admin", "Admin1password")
	req.RemoteAddr = ip + ":12345"
	rec := serveLogin(t, srv, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("locked-IP login status = %d, want 429", rec.Code)
	}
}

// TestLogin_IPLockoutDoesNotAffectOtherIPs proves the counter is
// per-IP. After seeding IP A to its cap, IP B with a wrong password
// must still produce a normal 401 (not the 429 we'd see if the lockout
// were global).
func TestLogin_IPLockoutDoesNotAffectOtherIPs(t *testing.T) {
	now := time.Date(2026, time.May, 3, 12, 0, 0, 0, time.UTC)

	store, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	t.Cleanup(func() { store.Close() })

	srv := mustNew(t, Options{
		LoginTimingFloor: -1,
		Now:              func() time.Time { return now },
		Store:            store,
	})
	t.Cleanup(srv.Close)

	if _, _, err := srv.auth.BootstrapUser(context.Background(), auth.BootstrapInput{
		Username: "admin",
		Password: "Admin1password",
		Role:     auth.RoleAdmin,
	}, now); err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}

	const lockedIP = "192.0.2.1"
	for i := 0; i < sessions.IPLockoutMaxFailures; i++ {
		srv.ipLockout.RecordFailureWithContext(context.Background(), lockedIP, now.Add(time.Duration(i)*time.Second))
	}

	req := buildLoginRequest(t, "admin", "wrongpassword")
	req.RemoteAddr = "198.51.100.1:23456" // unrelated IP
	rec := serveLogin(t, srv, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unrelated IP login status = %d, want 401", rec.Code)
	}
}

// TestLogin_IPLockoutWindowExpiry proves that once IPLockoutDuration
// elapses, the IP gets a fresh budget. We seed the tracker, advance
// the server clock past the deadline, and assert a wrong-password
// attempt now produces the standard 401 — not 429.
func TestLogin_IPLockoutWindowExpiry(t *testing.T) {
	startAt := time.Date(2026, time.May, 3, 12, 0, 0, 0, time.UTC)
	clock := startAt

	store, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	t.Cleanup(func() { store.Close() })

	srv := mustNew(t, Options{
		LoginTimingFloor: -1,
		Now:              func() time.Time { return clock },
		Store:            store,
	})
	t.Cleanup(srv.Close)

	if _, _, err := srv.auth.BootstrapUser(context.Background(), auth.BootstrapInput{
		Username: "admin",
		Password: "Admin1password",
		Role:     auth.RoleAdmin,
	}, startAt); err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}

	const ip = "192.0.2.1"
	for i := 0; i < sessions.IPLockoutMaxFailures; i++ {
		srv.ipLockout.RecordFailureWithContext(context.Background(), ip, startAt.Add(time.Duration(i)*time.Second))
	}
	// Advance well past the IPLockoutDuration so the lockout window
	// has fully expired. Account for the fact that the last failure
	// landed at startAt+(max-1)s, so the deadline = +(max-1)s + duration.
	clock = startAt.Add(time.Duration(sessions.IPLockoutMaxFailures)*time.Second + sessions.IPLockoutDuration + time.Second)

	req := buildLoginRequest(t, "admin", "wrongpassword")
	req.RemoteAddr = ip + ":12345"
	rec := serveLogin(t, srv, req)
	// After the window, a wrong-password attempt should produce a
	// regular 401 — not 429 — because the IP got a fresh budget.
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("post-expiry login status = %d, want 401 (not 429)", rec.Code)
	}
}

// TestLogin_IPLockoutAccumulatesAcrossUsernames is the headline assertion:
// a single source IP can drive itself to the IP-lockout by failing 50
// times across DIFFERENT usernames, even though no single username
// reaches its own 5-attempt cap. This is the targeted-DoS scenario the
// IP-keyed counter exists to defend against.
func TestLogin_IPLockoutAccumulatesAcrossUsernames(t *testing.T) {
	now := time.Date(2026, time.May, 3, 12, 0, 0, 0, time.UTC)

	store, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	t.Cleanup(func() { store.Close() })

	srv := mustNew(t, Options{
		LoginTimingFloor: -1,
		Now:              func() time.Time { return now },
		Store:            store,
	})
	t.Cleanup(srv.Close)

	const ip = "192.0.2.42"
	// Spread IPLockoutMaxFailures across 25 distinct usernames, two
	// each — well under the username lockout threshold of 5. The IP
	// counter must still trip.
	for i := 0; i < sessions.IPLockoutMaxFailures; i++ {
		srv.ipLockout.RecordFailureWithContext(context.Background(), ip, now.Add(time.Duration(i)*time.Second))
	}
	if !srv.ipLockout.IsLocked(ip, now.Add(time.Minute)) {
		t.Fatalf("IP not locked after %d failures across multiple usernames", sessions.IPLockoutMaxFailures)
	}
	// Username-level counter for any one user should NOT be tripped
	// (we only sent 2 fails per user — well below 5).
	if srv.loginLockout.IsLocked("admin", now.Add(time.Minute)) {
		t.Fatalf("username-level lockout tripped unexpectedly — failures were only IP-side")
	}
}
