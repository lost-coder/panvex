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

// SetSubscriptionListener configures the public /sub listener. addr is the
// bind address (e.g. ":8081"); baseURL is the public origin used to build
// shareable links in the admin UI (e.g. "https://sub.example.com").
func (s *Server) SetSubscriptionListener(addr, baseURL string) {
	s.subscriptionAddr = strings.TrimSpace(addr)
	s.subscriptionBaseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
}

// SubscriptionBaseURL is read by the admin layer (Plan 3) to build the URL.
func (s *Server) SubscriptionBaseURL() string { return s.subscriptionBaseURL }

func (s *Server) subscriptionListenerEnabled() bool { return s.subscriptionAddr != "" }

// newSubscriptionRouter builds the public router: a per-IP rate limiter in
// front of GET /sub/{token}. Limits brute-force token scanning.
func (s *Server) newSubscriptionRouter() http.Handler {
	r := chi.NewRouter()
	r.Use(newIPRateLimiter(rate.Limit(5), 20)) // ~5 req/s/IP, burst 20
	r.Get("/sub/{token}", s.handleSubscriptionPage())
	r.NotFound(func(w http.ResponseWriter, _ *http.Request) {
		s.writeSubscriptionInactive(w, http.StatusNotFound)
	})
	return r
}

// newIPRateLimiter is a minimal token-bucket limiter keyed by client IP.
//
// NOTE: the buckets map is unbounded — one entry per unique IP. Acceptable
// for v1 (the listener is not exposed to open internet without a proxy), but
// a future hardening pass should evict idle entries via a time-keyed LRU.
func newIPRateLimiter(every rate.Limit, burst int) func(http.Handler) http.Handler {
	var mu sync.Mutex
	buckets := make(map[string]*rate.Limiter)
	limiterFor := func(ip string) *rate.Limiter {
		mu.Lock()
		defer mu.Unlock()
		lim, ok := buckets[ip]
		if !ok {
			lim = rate.NewLimiter(every, burst)
			buckets[ip] = lim
		}
		return lim
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip, _, err := net.SplitHostPort(r.RemoteAddr)
			if err != nil {
				ip = r.RemoteAddr
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
				s.logger.Error("subscription listener exited",
					"err", serveErr,
					"alert", "subscription_listener_exited",
				)
			}
		}
	}()
	if s.logger != nil {
		s.logger.Info("subscription listener started",
			"addr", listener.Addr().String(),
		)
	}
	return listener.Addr(), srv.Shutdown, nil
}
