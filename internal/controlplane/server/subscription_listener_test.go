package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"golang.org/x/time/rate"
)

func TestIPRateLimiterBlocksBurst(t *testing.T) {
	mw := newIPRateLimiter(rate.Limit(1), 2)
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
	mw := newIPRateLimiter(rate.Limit(0.001), 1)
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
