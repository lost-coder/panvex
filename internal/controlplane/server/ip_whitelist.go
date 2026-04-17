package server

import (
	"net"
	"net/http"
)

// ipWhitelistMiddleware enforces that the resolved client IP is contained in
// at least one of the allowed CIDRs. Client IP resolution goes through
// resolveTrustedClientIP, which honours trustedProxyCIDRs to decide whether
// X-Forwarded-For is allowed to override r.RemoteAddr. This closes the
// leftmost-XFF bypass: with no trusted proxy configured, only r.RemoteAddr is
// used; with trusted proxies configured, the rightmost untrusted hop wins.
func ipWhitelistMiddleware(allowed, trustedProxyCIDRs []*net.IPNet) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if len(allowed) == 0 {
				next.ServeHTTP(w, r)
				return
			}

			ip := resolveTrustedClientIP(r, trustedProxyCIDRs)
			if ip == nil {
				writeError(w, http.StatusForbidden, "forbidden")
				return
			}

			for _, cidr := range allowed {
				if cidr.Contains(ip) {
					next.ServeHTTP(w, r)
					return
				}
			}

			writeError(w, http.StatusForbidden, "forbidden")
		})
	}
}
