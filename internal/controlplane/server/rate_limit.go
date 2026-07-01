package server

import (
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

// withRateLimit wraps a handler with a per-key rate-limit gate.
// `scope` labels rejections in the panvex_ratelimit_rejected_total
// metric — must be one of rateLimitScopes (login, agent_bootstrap,
// sensitive, grpc_connect) so dashboards/alerts pre-init zero series.
func (s *Server) withRateLimit(limiter *fixedWindowRateLimiter, scope string, keyFn func(*http.Request) string) func(http.Handler) http.Handler {
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
				s.obs.ObserveRateLimitReject(scope)
				writeError(w, http.StatusTooManyRequests, "rate limit exceeded")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// requestClientRateLimitKey extracts a per-client key for rate limiting.
//
// Client identity is resolved via resolveTrustedClientIP (trusted_proxy.go),
// the same first-untrusted-hop algorithm ipWhitelistMiddleware uses, so the
// login-lockout/rate-limit path and the IP-whitelist path can never drift
// into two different notions of "client IP". This only works correctly when
// TrustedProxyCIDRs (in Options) includes every proxy/load-balancer CIDR
// that may appear as r.RemoteAddr. If TrustedProxyCIDRs is empty (and the
// proxy is not on loopback), all requests resolve to the proxy's own IP and
// share a single rate-limit/lockout bucket, effectively throttling the
// entire fleet as one client — see warnIfTrustedProxyMisconfigured and the
// production hard-fail in checkTrustedProxyMisconfigured.
func (s *Server) requestClientRateLimitKey(r *http.Request) string {
	if key := trustedClientIPString(r, s.trustedProxyCIDRs); key != "" {
		return key
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
