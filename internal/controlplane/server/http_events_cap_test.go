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

// TestHandleEvents_Returns429WhenUserAtCap pre-fills the WS connection
// limiter for the authenticated user up to maxWSConnsPerUser, then sends
// one more /events request. The handler must reject with 429 BEFORE
// attempting the WebSocket upgrade — that is the whole point of the cap:
// stop a goroutine-exhaustion attack at the HTTP layer, not after we
// commit to an upgraded connection.
func TestHandleEvents_Returns429WhenUserAtCap(t *testing.T) {
	now := time.Date(2026, time.May, 3, 12, 0, 0, 0, time.UTC)

	store, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	t.Cleanup(func() { store.Close() })

	srv := New(Options{
		LoginTimingFloor: -1,
		Now:              func() time.Time { return now },
		Store:            store,
	})
	t.Cleanup(srv.Close)

	user, _, err := srv.auth.BootstrapUser(context.Background(), auth.BootstrapInput{
		Username: "admin",
		Password: "Admin1password",
		Role:     auth.RoleAdmin,
	}, now)
	if err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}

	loginResp := performJSONRequest(t, srv, http.MethodPost, "/api/auth/login", map[string]string{
		"username": "admin",
		"password": "Admin1password",
	}, nil)
	if loginResp.Code != http.StatusOK {
		t.Fatalf("login status = %d, want 200", loginResp.Code)
	}
	cookies := loginResp.Result().Cookies()

	// Fill the per-user slot table to its cap. The eventsConnLimitKey
	// helper builds the same "user:<id>" key the handler will use.
	key := "user:" + user.ID
	for i := 0; i < maxWSConnsPerUser; i++ {
		if !srv.wsConnLimiter.acquire(key, maxWSConnsPerUser) {
			t.Fatalf("pre-fill #%d failed unexpectedly", i+1)
		}
	}
	t.Cleanup(func() {
		for i := 0; i < maxWSConnsPerUser; i++ {
			srv.wsConnLimiter.release(key)
		}
	})

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/events", nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("over-cap /events status = %d, want %d", rec.Code, http.StatusTooManyRequests)
	}

	// After releasing one slot, the next request must succeed at the
	// HTTP layer — i.e. it must NOT return 429. (We do not assert a
	// successful WebSocket upgrade because httptest's ResponseRecorder
	// doesn't hijack — but the absence of 429 proves the limiter
	// allowed the slot.)
	srv.wsConnLimiter.release(key)
	req2 := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/events", nil)
	for _, c := range cookies {
		req2.AddCookie(c)
	}
	rec2 := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec2, req2)
	if rec2.Code == http.StatusTooManyRequests {
		t.Fatalf("after release, /events still 429 — slot was not freed")
	}
}

// TestHandleEvents_429DoesNotConsumeSlot proves the rejected path leaves
// the counter unchanged. Without this guarantee, an attacker could fire
// repeated rejected requests to permanently parked the counter and lock
// the legitimate user out.
func TestHandleEvents_429DoesNotConsumeSlot(t *testing.T) {
	now := time.Date(2026, time.May, 3, 12, 0, 0, 0, time.UTC)

	store, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	t.Cleanup(func() { store.Close() })

	srv := New(Options{
		LoginTimingFloor: -1,
		Now:              func() time.Time { return now },
		Store:            store,
	})
	t.Cleanup(srv.Close)

	user, _, err := srv.auth.BootstrapUser(context.Background(), auth.BootstrapInput{
		Username: "admin",
		Password: "Admin1password",
		Role:     auth.RoleAdmin,
	}, now)
	if err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}

	loginResp := performJSONRequest(t, srv, http.MethodPost, "/api/auth/login", map[string]string{
		"username": "admin",
		"password": "Admin1password",
	}, nil)
	if loginResp.Code != http.StatusOK {
		t.Fatalf("login status = %d, want 200", loginResp.Code)
	}
	cookies := loginResp.Result().Cookies()

	key := "user:" + user.ID
	for i := 0; i < maxWSConnsPerUser; i++ {
		if !srv.wsConnLimiter.acquire(key, maxWSConnsPerUser) {
			t.Fatalf("pre-fill #%d failed unexpectedly", i+1)
		}
	}
	t.Cleanup(func() {
		for i := 0; i < maxWSConnsPerUser; i++ {
			srv.wsConnLimiter.release(key)
		}
	})

	// Hit /events ten times — every attempt must be rejected, and the
	// counter must stay pinned at the cap (no leak).
	for i := 0; i < 10; i++ {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/events", nil)
		for _, c := range cookies {
			req.AddCookie(c)
		}
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusTooManyRequests {
			t.Fatalf("attempt #%d status = %d, want 429", i+1, rec.Code)
		}
	}
	if got := srv.wsConnLimiter.snapshot(key); int(got) != maxWSConnsPerUser {
		t.Fatalf("counter after rejected attempts = %d, want %d", got, maxWSConnsPerUser)
	}
}
