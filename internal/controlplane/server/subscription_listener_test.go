package server

import (
	"context"
	"fmt"
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
// TTL (and past sweepInterval, since eviction now only runs on the
// amortized sweep — see TestIPRateLimiterSweepIsAmortized) via an injected
// clock, a fresh request from the same IP gets a brand new bucket (full
// burst available again) rather than reusing the rate-limited exhausted
// one.
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

	// Advance the clock well past ipBucketIdleTTL (and past sweepInterval,
	// so the amortized sweep is due) with no intervening traffic, then send
	// one more request. If eviction works, the bucket for this IP is gone
	// and a fresh one (full burst) is created.
	current = current.Add(ipBucketIdleTTL + time.Minute)
	if code := send(); code != http.StatusOK {
		t.Fatalf("request after idle TTL: got %d, want 200 (bucket should have been evicted and recreated)", code)
	}
}

// TestIPRateLimiterSweepIsAmortized proves the Important-severity follow-up
// fix: evictIdleLocked's O(n) full-map scan must not run on every request.
// It is amortized to at most once per sweepInterval: an entry that has
// already gone idle past ipBucketIdleTTL is NOT evicted by a flood of
// further accesses as long as those accesses land within the same
// sweepInterval window as the last sweep — it is only reclaimed once a
// request arrives after sweepInterval has elapsed since that last sweep.
//
// We drive this via observable behavior rather than an instrumented scan
// counter (there is no exported hook to count scans without changing
// production code): an exhausted bucket's 429 response is our proxy for
// "this bucket was never evicted." Note that probing an IP (calling send)
// always refreshes its lastAccess, so each IP used to prove "still stale"
// at a given instant may only be probed ONCE for that purpose — probing it
// again would itself un-stale it. The scenario below uses a dedicated
// checkIP, probed exactly once, right after the flood, to capture whether
// it survived.
func TestIPRateLimiterSweepIsAmortized(t *testing.T) {
	current := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	nowFn := func() time.Time { return current }

	// Burst=1 and a zero refill rate so a single request permanently
	// exhausts a bucket (it never refills on its own), making "does this IP
	// still have its original (exhausted) bucket" observable via the
	// resulting status code: 429 means the original bucket survived, 200
	// means it was evicted and replaced with a fresh one.
	mw := newIPRateLimiterWithClock(rate.Limit(0), 1, nil, nowFn)
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) }))

	send := func(ip string) int {
		rec := httptest.NewRecorder()
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/sub/x", nil)
		req.RemoteAddr = ip + ":4444"
		h.ServeHTTP(rec, req)
		return rec.Code
	}

	const checkIP = "203.0.113.50"
	if code := send(checkIP); code != http.StatusOK {
		t.Fatalf("checkIP first request: got %d, want 200", code)
	}
	// This access also runs the very first sweep (lastSweep starts at the
	// zero value, so the first request always sweeps an empty map) and sets
	// lastSweep = t0.
	t0 := current

	// Walk the clock forward one sweepInterval at a time, touching a fresh
	// "other" IP just past each boundary (elapsed-since-last-sweep just
	// over sweepInterval) so each step triggers its own sweep. checkIP's
	// idle time grows with each step but stays under ipBucketIdleTTL for
	// all but the last step, so none of these sweeps touch it — this just
	// walks the clock up to (but short of) the point where a sweep would
	// evict it, without yet testing amortization. checkIP itself is never
	// probed during this walk-up, so its lastAccess stays pinned at t0.
	cyclesBeforeTTL := int(ipBucketIdleTTL/sweepInterval) - 1
	for i := 0; i < cyclesBeforeTTL; i++ {
		current = current.Add(sweepInterval + time.Second)
		ip := fmt.Sprintf("198.51.99.%d", i%256)
		if code := send(ip); code != http.StatusOK {
			t.Fatalf("walk-up cycle %d: got %d, want 200", i, code)
		}
	}
	lastSweepAt := current // a sweep just ran on the final walk-up iteration.
	if current.Sub(t0) >= ipBucketIdleTTL {
		t.Fatalf("test setup invariant violated: checkIP already past ipBucketIdleTTL after walk-up (idle=%v) — walk-up must stay under TTL", current.Sub(t0))
	}

	// Cross ipBucketIdleTTL (relative to t0) now, but stay strictly inside
	// ONE sweepInterval window measured from lastSweepAt. Because
	// ipBucketIdleTTL (10m) is far larger than sweepInterval (1m), this gap
	// is well under sweepInterval, so no sweep is due — yet checkIP is now
	// definitely overdue for eviction.
	remaining := ipBucketIdleTTL - current.Sub(t0)
	current = current.Add(remaining + time.Second)
	if current.Sub(t0) <= ipBucketIdleTTL {
		t.Fatalf("test setup invariant violated: checkIP not yet past ipBucketIdleTTL (idle=%v)", current.Sub(t0))
	}
	if current.Sub(lastSweepAt) >= sweepInterval {
		t.Fatalf("test setup invariant violated: already past sweepInterval (elapsed=%v) — flood would trivially trigger a sweep", current.Sub(lastSweepAt))
	}

	// Flood with many distinct-IP accesses at this exact instant (no further
	// clock advance), all landing inside the still-open sweepInterval
	// window. Per the fix, none of these may trigger evictIdleLocked, so
	// checkIP's now-overdue bucket must survive all of them. We deliberately
	// do NOT probe checkIP during the flood (that would refresh its
	// lastAccess and invalidate the point).
	const floodIPs = 500
	for i := 0; i < floodIPs; i++ {
		ip := fmt.Sprintf("198.51.%d.%d", 100+i/256, i%256)
		if code := send(ip); code != http.StatusOK {
			t.Fatalf("flood IP %d (%s): got %d, want 200 (first request for a fresh bucket)", i, ip, code)
		}
	}

	// The decisive assertion, taken immediately after the flood and BEFORE
	// crossing into the next sweepInterval: checkIP has been idle for well
	// over ipBucketIdleTTL (checked above) and the map has just absorbed a
	// 500-request flood of distinct IPs, yet elapsed-since-last-sweep is
	// still under sweepInterval (checked above). If evictIdleLocked ran on
	// every request (the bug this fix addresses), checkIP would have been
	// evicted the instant it crossed ipBucketIdleTTL, and this probe would
	// see a fresh bucket (200). Because the scan is amortized, it survives
	// (429) — proving the full-map scan ran at most once across the entire
	// walk-up-plus-flood sequence above, not on every request. (Eventual
	// eviction once a sweep is actually due is already covered by
	// TestIPRateLimiterEvictsIdleBuckets.)
	if code := send(checkIP); code != http.StatusTooManyRequests {
		t.Fatalf("checkIP survives flood inside sweepInterval: got %d, want 429 (sweep must not have run mid-flood)", code)
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
