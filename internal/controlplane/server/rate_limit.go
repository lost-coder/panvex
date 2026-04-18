package server

import (
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/sessions"
)

// Task P3-ARCH-01c: the pure fixed-window rate limiter now lives in
// controlplane/sessions. The HTTP middleware + request-keying helpers
// stay here because they reach into *Server state (trustedProxyCIDRs,
// auth context, s.now(), writeError) — the task deliberately keeps
// transport glue in server/.

type fixedWindowRateLimiter = sessions.RateLimiter

func newFixedWindowRateLimiter(limit int, window time.Duration) *fixedWindowRateLimiter {
	return sessions.NewRateLimiter(limit, window)
}

func (s *Server) withRateLimit(limiter *fixedWindowRateLimiter, keyFn func(*http.Request) string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if limiter == nil {
				next.ServeHTTP(w, r)
				return
			}
			key := ""
			if keyFn != nil {
				key = keyFn(r)
			}
			if !limiter.Allow(key, s.now()) {
				writeError(w, http.StatusTooManyRequests, "rate limit exceeded")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// requestClientRateLimitKey extracts a per-client key for rate limiting.
//
// When the server sits behind a reverse proxy, the function uses the rightmost
// X-Forwarded-For entry — the hop appended by the last trusted proxy — as the
// client identity. This only works correctly when TrustedProxyCIDRs (in
// Options) includes every proxy/load-balancer CIDR that may appear as
// r.RemoteAddr. If TrustedProxyCIDRs is empty (and the proxy is not on
// loopback), all requests are keyed by the proxy's own IP and share a single
// rate-limit bucket, effectively rate-limiting the entire fleet as one client.
func (s *Server) requestClientRateLimitKey(r *http.Request) string {
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		// Use the rightmost X-Forwarded-For entry — the last hop appended
		// by a trusted proxy. The leftmost entry is attacker-controlled.
		parts := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
		forwardedFor := strings.TrimSpace(parts[len(parts)-1])
		if forwardedFor != "" && s.remoteAddrTrustsForwardedFor(host) {
			return forwardedFor
		}
		if strings.TrimSpace(host) != "" {
			return host
		}
	}

	return strings.TrimSpace(r.RemoteAddr)
}

// requestSessionRateLimitKey returns a per-user key when the request carries
// an authenticated session, otherwise falls back to the per-IP rate-limit
// key. Used by sensitiveRateLimiter so one authenticated user cannot
// brute-force TOTP-enable codes or spam enrollment tokens. "user:" / "ip:"
// prefixes guarantee there is no collision between an IP whose literal value
// happens to match a user ID.
func (s *Server) requestSessionRateLimitKey(r *http.Request) string {
	if session, _, ok := requestAuthContext(r); ok && session.UserID != "" {
		return "user:" + session.UserID
	}
	return "ip:" + s.requestClientRateLimitKey(r)
}

// remoteAddrTrustsForwardedFor reports whether the given remote address
// belongs to a trusted proxy whose X-Forwarded-For header should be used
// for client identification. Loopback addresses are always trusted;
// additional ranges can be configured via TrustedProxyCIDRs.
func (s *Server) remoteAddrTrustsForwardedFor(host string) bool {
	if strings.EqualFold(strings.TrimSpace(host), "localhost") {
		return true
	}

	ip := net.ParseIP(strings.TrimSpace(host))
	if ip == nil {
		return false
	}
	if ip.IsLoopback() {
		return true
	}
	for _, cidr := range s.trustedProxyCIDRs {
		if cidr.Contains(ip) {
			return true
		}
	}
	return false
}
