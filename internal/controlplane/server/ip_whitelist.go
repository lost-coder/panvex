package server

import (
	"net"
	"net/http"
	"strings"
)

func ipWhitelistMiddleware(allowed []*net.IPNet) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if len(allowed) == 0 {
				next.ServeHTTP(w, r)
				return
			}

			clientIP := resolveClientIP(r)
			ip := net.ParseIP(clientIP)
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

func resolveClientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.SplitN(xff, ",", 2)
		if ip := strings.TrimSpace(parts[0]); ip != "" {
			return ip
		}
	}

	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
