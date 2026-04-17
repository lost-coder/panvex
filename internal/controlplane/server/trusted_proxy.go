package server

import (
	"net"
	"net/http"
	"strings"
)

// resolveTrustedClientIP determines the real client IP for r given a set of
// trusted-proxy CIDRs. It is used by both the IP whitelist middleware and the
// rate limiter so they cannot drift out of sync.
//
// Algorithm:
//   - Start with the TCP peer (r.RemoteAddr). If the peer itself is not in
//     trustedCIDRs and is not loopback, return it: X-Forwarded-For entries
//     from an untrusted peer are attacker-controlled and must be ignored.
//   - Otherwise walk X-Forwarded-For right-to-left. Each hop is normalised
//     (port stripped for IPv4 `a.b.c.d:port` and IPv6 `[...]:port`). The first
//     hop that is *not* in trustedCIDRs is the client.
//   - If every hop is in trustedCIDRs, the leftmost entry is the originator
//     (and the caller should treat that result as advisory).
//   - X-Real-IP is intentionally ignored — we trust exactly one header.
func resolveTrustedClientIP(r *http.Request, trustedCIDRs []*net.IPNet) net.IP {
	peerHost, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		peerHost = strings.TrimSpace(r.RemoteAddr)
	}
	peerIP := net.ParseIP(peerHost)

	if !peerIsTrusted(peerIP, trustedCIDRs) {
		// Peer is untrusted: ignore XFF entirely and use the TCP source.
		return peerIP
	}

	// Peer is a trusted proxy. Walk XFF right-to-left for the first
	// untrusted hop; fall back to the leftmost hop (originator) when the
	// whole chain is trusted.
	raw := r.Header.Get("X-Forwarded-For")
	if raw == "" {
		return peerIP
	}
	hops := strings.Split(raw, ",")
	var firstHop net.IP
	for i := len(hops) - 1; i >= 0; i-- {
		hop := stripPort(strings.TrimSpace(hops[i]))
		if hop == "" {
			continue
		}
		ip := net.ParseIP(hop)
		if ip == nil {
			continue
		}
		if i == 0 {
			firstHop = ip
		}
		if !peerIsTrusted(ip, trustedCIDRs) {
			return ip
		}
	}
	// Whole chain is trusted — return the leftmost hop (the originator) as
	// the best-effort client identity.
	if firstHop != nil {
		return firstHop
	}
	return peerIP
}

// peerIsTrusted reports whether ip is a loopback address or is contained in
// any of the configured trusted proxy CIDRs. Loopback is always trusted so
// that local reverse-proxy topologies work without extra configuration.
func peerIsTrusted(ip net.IP, trustedCIDRs []*net.IPNet) bool {
	if ip == nil {
		return false
	}
	if ip.IsLoopback() {
		return true
	}
	for _, cidr := range trustedCIDRs {
		if cidr.Contains(ip) {
			return true
		}
	}
	return false
}

// stripPort removes a trailing :port suffix from an IPv4 address or an IPv6
// address in bracketed form (`[2001:db8::1]:8080`). Bare IPv6 addresses — which
// also contain colons — are returned unchanged.
func stripPort(s string) string {
	if s == "" {
		return ""
	}
	if strings.HasPrefix(s, "[") {
		// Bracketed IPv6, possibly with :port after the closing bracket.
		if idx := strings.Index(s, "]"); idx > 0 {
			inside := s[1:idx]
			return inside
		}
		return s
	}
	// Heuristic: IPv4 has exactly one colon (host:port); raw IPv6 has many.
	if strings.Count(s, ":") == 1 {
		if host, _, err := net.SplitHostPort(s); err == nil {
			return host
		}
	}
	return s
}

// trustedClientIPString returns the string form of resolveTrustedClientIP
// suitable for use as a rate-limit bucket key or as input to net.ParseIP by
// the IP whitelist middleware. A nil IP yields an empty string so the caller
// can decide how to handle missing data.
func trustedClientIPString(r *http.Request, trustedCIDRs []*net.IPNet) string {
	ip := resolveTrustedClientIP(r, trustedCIDRs)
	if ip == nil {
		return ""
	}
	return ip.String()
}
