package server

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"golang.org/x/time/rate"
)

func TestIPRateLimiterBlocksBurst(t *testing.T) {
	mw := newIPRateLimiter(rate.Limit(1), 2, nil)
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) }))
	var lastCode int
	for i := 0; i < 5; i++ {
		rec := httptest.NewRecorder()
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/sub/x", nil)
		req.RemoteAddr = "203.0.113.7:5555"
		h.ServeHTTP(rec, req)
		lastCode = rec.Code
	}
	if lastCode != http.StatusTooManyRequests {
		t.Fatalf("after burst, last code = %d, want 429", lastCode)
	}
}

func TestIPRateLimiterIndependentBuckets(t *testing.T) {
	// Two different IPs each get their own bucket.
	// Burst=1 means the first request is allowed but the second (same IP) is not.
	mw := newIPRateLimiter(rate.Limit(0.001), 1, nil)
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) }))

	sendReq := func(ip string) int {
		rec := httptest.NewRecorder()
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/sub/x", nil)
		req.RemoteAddr = ip + ":1234"
		h.ServeHTTP(rec, req)
		return rec.Code
	}

	// Both IPs get their first request through.
	if code := sendReq("203.0.113.1"); code != http.StatusOK {
		t.Fatalf("IP-A first request: got %d, want 200", code)
	}
	if code := sendReq("203.0.113.2"); code != http.StatusOK {
		t.Fatalf("IP-B first request: got %d, want 200", code)
	}

	// Second request from IP-A should be rate-limited (burst exhausted).
	if code := sendReq("203.0.113.1"); code != http.StatusTooManyRequests {
		t.Fatalf("IP-A second request: got %d, want 429", code)
	}

	// IP-B's second request is also rate-limited independently.
	if code := sendReq("203.0.113.2"); code != http.StatusTooManyRequests {
		t.Fatalf("IP-B second request: got %d, want 429", code)
	}
}

// TestIPRateLimiterXFFDistinctClients proves the 3.7 fix: behind a trusted
// proxy, two requests sharing the same RemoteAddr (the proxy) but carrying
// different X-Forwarded-For client IPs land in distinct buckets rather than
// colliding on the proxy's own address. Before the fix, newIPRateLimiter
// keyed on raw net.SplitHostPort(r.RemoteAddr) and ignored XFF entirely, so
// both "clients" would share one bucket and could lock each other out.
func TestIPRateLimiterXFFDistinctClients(t *testing.T) {
	_, trustedProxyCIDR, err := net.ParseCIDR("203.0.113.0/24")
	if err != nil {
		t.Fatalf("ParseCIDR: %v", err)
	}
	trusted := []*net.IPNet{trustedProxyCIDR}

	// Burst=1: the first request from a given resolved client succeeds, the
	// second from the SAME resolved client is rejected.
	mw := newIPRateLimiter(rate.Limit(0.001), 1, trusted)
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) }))

	sendReq := func(xff string) int {
		rec := httptest.NewRecorder()
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/sub/x", nil)
		req.RemoteAddr = "203.0.113.7:5555" // trusted proxy peer
		req.Header.Set("X-Forwarded-For", xff)
		h.ServeHTTP(rec, req)
		return rec.Code
	}

	// Two distinct real clients behind the same trusted proxy.
	if code := sendReq("198.51.100.1"); code != http.StatusOK {
		t.Fatalf("client A first request: got %d, want 200", code)
	}
	if code := sendReq("198.51.100.2"); code != http.StatusOK {
		t.Fatalf("client B first request (distinct bucket): got %d, want 200", code)
	}
	// Second request from client A is now rate-limited — proves it has its
	// own bucket and burst was consumed, not shared with client B.
	if code := sendReq("198.51.100.1"); code != http.StatusTooManyRequests {
		t.Fatalf("client A second request: got %d, want 429", code)
	}
	// Client B's own second request is independently rate-limited too.
	if code := sendReq("198.51.100.2"); code != http.StatusTooManyRequests {
		t.Fatalf("client B second request: got %d, want 429", code)
	}
}

// TestIPRateLimiterEvictsIdleBuckets proves the 3.7 unbounded-map fix: a
// bucket untouched for longer than ipBucketIdleTTL is evicted so the map
// does not grow forever. Verified indirectly — after "advancing" past the
// TTL via an injected clock, a fresh request from the same IP gets a brand
// new bucket (full burst available again) rather than reusing the
// rate-limited exhausted one.
func TestIPRateLimiterEvictsIdleBuckets(t *testing.T) {
	current := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	nowFn := func() time.Time { return current }

	mw := newIPRateLimiterWithClock(rate.Limit(0.001), 1, nil, nowFn)
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) }))

	send := func() int {
		rec := httptest.NewRecorder()
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/sub/x", nil)
		req.RemoteAddr = "203.0.113.9:4444"
		h.ServeHTTP(rec, req)
		return rec.Code
	}

	if code := send(); code != http.StatusOK {
		t.Fatalf("first request: got %d, want 200", code)
	}
	if code := send(); code != http.StatusTooManyRequests {
		t.Fatalf("second request (burst exhausted): got %d, want 429", code)
	}

	// Advance the clock well past ipBucketIdleTTL with no intervening
	// traffic, then send one more request. If eviction works, the bucket
	// for this IP is gone and a fresh one (full burst) is created.
	current = current.Add(ipBucketIdleTTL + time.Minute)
	if code := send(); code != http.StatusOK {
		t.Fatalf("request after idle TTL: got %d, want 200 (bucket should have been evicted and recreated)", code)
	}
}

// TestSubscriptionBaseURL_LiveSettingOverridesEnv proves that SubscriptionBaseURL()
// returns the live dashboard setting when set, and falls back to the env-seeded
// field when the setting is empty.
func TestSubscriptionBaseURL_LiveSettingOverridesEnv(t *testing.T) {
	srv := testServerWithSQLite(t, time.Date(2026, 6, 14, 10, 0, 0, 0, time.UTC))
	ctx := context.Background()

	// Seed the env-seeded field (as startup code would via SetSubscriptionListener).
	srv.SetSubscriptionListener(":8081", "https://env.example.com")
	if got := srv.SubscriptionBaseURL(); got != "https://env.example.com" {
		t.Fatalf("before live setting: SubscriptionBaseURL() = %q, want %q", got, "https://env.example.com")
	}

	// Write the live setting through the store — this should take precedence.
	if err := srv.settings.Put(ctx, map[string]string{"subscription.public_base_url": "https://live.example.com/"}, "test"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if got := srv.SubscriptionBaseURL(); got != "https://live.example.com" {
		t.Fatalf("after live setting: SubscriptionBaseURL() = %q, want %q (trailing slash must be trimmed)", got, "https://live.example.com")
	}

	// Clear the live setting — should fall back to the env-seeded value.
	if err := srv.settings.Put(ctx, map[string]string{"subscription.public_base_url": ""}, "test"); err != nil {
		t.Fatalf("Put (clear): %v", err)
	}
	if got := srv.SubscriptionBaseURL(); got != "https://env.example.com" {
		t.Fatalf("after clearing live setting: SubscriptionBaseURL() = %q, want %q", got, "https://env.example.com")
	}
}
