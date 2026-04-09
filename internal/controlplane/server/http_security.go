package server

import (
	"net/http"
	"net/url"
	"strings"
)

// securityHeaders applies standard security response headers to every HTTP response.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Frame-Options", "DENY")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		h.Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; connect-src 'self' wss:; img-src 'self' data:; frame-ancestors 'none'")
		h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		next.ServeHTTP(w, r)
	})
}

// csrfOriginCheck rejects state-changing requests (POST, PUT, DELETE, PATCH) whose
// Origin or Referer header does not match the request host. This prevents cross-site
// request forgery for cookie-authenticated APIs without requiring per-request tokens.
//
// Safe methods (GET, HEAD, OPTIONS) and requests with no Origin/Referer (e.g. agent
// bootstrap calls) are allowed through.
func csrfOriginCheck(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}

		origin := r.Header.Get("Origin")
		if origin == "" {
			// Fall back to Referer when Origin is absent (e.g. older browsers).
			origin = r.Header.Get("Referer")
		}
		if origin == "" {
			// No origin information. If the request carries a session cookie it
			// originates from a browser that stripped Origin/Referer — block it.
			// Non-browser clients (agent mTLS, API keys) never send the cookie.
			if _, err := r.Cookie(sessionCookieName); err == nil {
				writeError(w, http.StatusForbidden, "missing origin header for cookie-authenticated request")
				return
			}
			next.ServeHTTP(w, r)
			return
		}

		if originMatchesHost(origin, r.Host) {
			next.ServeHTTP(w, r)
			return
		}

		writeError(w, http.StatusForbidden, "cross-origin request blocked")
	})
}

// originMatchesHost checks whether the host portion of the origin URL matches
// the request host. Port differences are tolerated when the origin uses a default
// port (80/443) that the browser omits.
func originMatchesHost(origin string, requestHost string) bool {
	parsed, err := url.Parse(origin)
	if err != nil {
		return false
	}

	originHost := parsed.Host
	if originHost == "" {
		originHost = parsed.Path
	}

	// Strip default ports for comparison.
	requestHost = stripDefaultPort(requestHost)
	originHost = stripDefaultPort(originHost)

	return strings.EqualFold(originHost, requestHost)
}

func stripDefaultPort(host string) string {
	if strings.HasSuffix(host, ":80") || strings.HasSuffix(host, ":443") {
		idx := strings.LastIndex(host, ":")
		return host[:idx]
	}
	return host
}
