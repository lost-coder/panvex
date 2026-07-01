package server

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"golang.org/x/time/rate"
)

// ipBucketIdleTTL is how long an /sub rate-limit bucket may sit idle before
// it becomes eligible for eviction (checked opportunistically on the next
// access to the bucket map). Chosen well above the burst-refill window (a
// few seconds) so a legitimately slow client never loses its bucket
// mid-session, but short enough that a scan sweeping many distinct
// XFF-spoofed IPs does not pin memory indefinitely (3.7).
const ipBucketIdleTTL = 10 * time.Minute

// SetSubscriptionListener configures the public /sub listener. addr is the
// bind address (e.g. ":8081"); baseURL is the public origin used to build
// shareable links in the admin UI (e.g. "https://sub.example.com").
func (s *Server) SetSubscriptionListener(addr, baseURL string) {
	s.subscriptionAddr = strings.TrimSpace(addr)
	s.subscriptionBaseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
}

// SubscriptionBaseURL is read by the admin layer (Plan 3) to build the URL.
// It prefers the live dashboard setting over the env-seeded field, so the
// operator can change the public origin without restarting the panel.
func (s *Server) SubscriptionBaseURL() string {
	if s.settings != nil {
		if u := s.settings.SubscriptionPublicBaseURL(); u != "" {
			return strings.TrimRight(u, "/")
		}
	}
	return s.subscriptionBaseURL
}

func (s *Server) subscriptionListenerEnabled() bool { return s.subscriptionAddr != "" }

// newSubscriptionRouter builds the public router: a per-IP rate limiter in
// front of GET /sub/{token}. Limits brute-force token scanning.
func (s *Server) newSubscriptionRouter() http.Handler {
	r := chi.NewRouter()
	// Keyed on the trusted-proxy-resolved client IP (not raw RemoteAddr) so
	// that behind a reverse proxy distinct real clients land in distinct
	// buckets instead of all colliding on the proxy's own address (3.7).
	r.Use(newIPRateLimiter(rate.Limit(5), 20, s.trustedProxyCIDRs)) // ~5 req/s/IP, burst 20
	r.Get("/sub/{token}", s.handleSubscriptionPage())
	r.NotFound(func(w http.ResponseWriter, _ *http.Request) {
		s.writeSubscriptionInactive(w, http.StatusNotFound)
	})
	return r
}

// ipRateLimiterBucket pairs a token bucket with the wall-clock time of its
// last access, so the sweeper can evict entries nobody has touched in a
// while without maintaining a separate LRU structure.
type ipRateLimiterBucket struct {
	limiter    *rate.Limiter
	lastAccess time.Time
}

// newIPRateLimiter is a token-bucket limiter keyed by the trusted-proxy
// resolved client IP (resolveTrustedClientIP) — the same resolution the
// login-lockout and IP-whitelist paths use, so a request behind a reverse
// proxy is bucketed by the real client rather than colliding with every
// other client on the proxy's own RemoteAddr.
func newIPRateLimiter(every rate.Limit, burst int, trustedCIDRs []*net.IPNet) func(http.Handler) http.Handler {
	return newIPRateLimiterWithClock(every, burst, trustedCIDRs, time.Now)
}

// newIPRateLimiterWithClock is newIPRateLimiter with an injectable clock so
// tests can advance "time" deterministically instead of sleeping past
// ipBucketIdleTTL.
//
// The buckets map grows with the number of distinct clients seen; idle
// entries (no request for longer than ipBucketIdleTTL) are evicted
// opportunistically on every access — every call to limiterFor first sweeps
// the map for stale entries — so the map cannot grow unbounded even under
// sustained XFF-spoofed scanning (3.7). No background goroutine is needed:
// as long as *some* traffic keeps arriving (which is the only scenario where
// unbounded growth is a risk in the first place), the opportunistic sweep on
// each request keeps the map bounded to roughly "distinct clients active
// within the last ipBucketIdleTTL".
func newIPRateLimiterWithClock(every rate.Limit, burst int, trustedCIDRs []*net.IPNet, nowFn func() time.Time) func(http.Handler) http.Handler {
	var mu sync.Mutex
	buckets := make(map[string]*ipRateLimiterBucket)

	evictIdleLocked := func(now time.Time) {
		for ip, bucket := range buckets {
			if now.Sub(bucket.lastAccess) > ipBucketIdleTTL {
				delete(buckets, ip)
			}
		}
	}

	limiterFor := func(ip string) *rate.Limiter {
		now := nowFn()
		mu.Lock()
		defer mu.Unlock()
		evictIdleLocked(now)
		bucket, ok := buckets[ip]
		if !ok {
			bucket = &ipRateLimiterBucket{limiter: rate.NewLimiter(every, burst)}
			buckets[ip] = bucket
		}
		bucket.lastAccess = now
		return bucket.limiter
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := trustedClientIPString(r, trustedCIDRs)
			if ip == "" {
				ip = strings.TrimSpace(r.RemoteAddr)
			}
			if !limiterFor(ip).Allow() {
				w.Header().Set("Retry-After", "1")
				http.Error(w, "rate limited", http.StatusTooManyRequests)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// StartSubscriptionListener binds and serves the public /sub listener. Mirrors
// StartPprofListener. Returns the bound addr and a shutdown func.
func (s *Server) StartSubscriptionListener(ctx context.Context) (net.Addr, func(context.Context) error, error) {
	if !s.subscriptionListenerEnabled() {
		return nil, nil, errors.New("subscription listener not configured")
	}
	var lc net.ListenConfig
	listener, err := lc.Listen(ctx, "tcp", s.subscriptionAddr)
	if err != nil {
		return nil, nil, err
	}
	srv := &http.Server{
		Handler:           s.newSubscriptionRouter(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	go func() {
		if serveErr := srv.Serve(listener); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			if s.logger != nil {
				s.logger.ErrorContext(ctx, "subscription listener exited",
					"err", serveErr,
					"alert", "subscription_listener_exited",
				)
			}
		}
	}()
	if s.logger != nil {
		s.logger.InfoContext(ctx, "subscription listener started",
			"addr", listener.Addr().String(),
		)
	}
	return listener.Addr(), srv.Shutdown, nil
}
