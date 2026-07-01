package server

import (
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strings"
)

// ErrTrustedProxyMisconfiguredProd reports that the panel is bound to a
// public (non-loopback) HTTP address with no TrustedProxyCIDRs configured
// while PANVEX_ENV=production. Left unfixed, X-Forwarded-For is silently
// ignored and every request — from the login handler's IP lockout, the
// generic rate limiter, and the IP whitelist middleware alike — resolves
// to the reverse proxy's own address. That collapses every client into a
// single shared bucket: a fleet-wide lockout (one attacker can lock out
// every legitimate operator) and a bypass of per-attacker throttling (an
// attacker's own requests average in with everyone else's). Mirrors the
// ErrInsecure*Prod fail-loud pattern in controlplane/config/storage.go —
// production must not silently start with a broken security boundary.
var ErrTrustedProxyMisconfiguredProd = errors.New("panel is bound to a public address with no trusted_proxy_cidrs configured; PANVEX_ENV=production requires PANVEX_TRUSTED_PROXY_CIDRS to be set (or bind to loopback)")

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
	// Q3.U-S-20: cap the chain length. A malicious upstream might
	// stuff thousands of synthetic hops into XFF, both to burn CPU
	// in this walk and to dilute the right-to-left "first untrusted
	// hop" search. Real deployments stack at most a handful of L7
	// proxies; 16 is a comfortable ceiling.
	const maxXFFHops = 16
	hops := strings.Split(raw, ",")
	if len(hops) > maxXFFHops {
		hops = hops[len(hops)-maxXFFHops:]
	}
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

// bindAddrIsPublic reports whether bindAddr is a non-loopback HTTP listen
// address — i.e. one that a reverse proxy (or a direct external client)
// could plausibly connect to over a network hop, as opposed to a loopback
// bind that only accepts local connections. An empty host (e.g. ":8080")
// means "listen on all interfaces", which is public.
//
// Shared by warnIfTrustedProxyMisconfigured (dev: WARN) and
// checkTrustedProxyMisconfigured (prod: hard error) so the two call sites
// can never disagree on what counts as "public".
func bindAddrIsPublic(bindAddr string) bool {
	host, _, err := net.SplitHostPort(bindAddr)
	if err != nil {
		host = bindAddr
	}
	host = strings.TrimSpace(host)
	// SplitHostPort returns the bare IPv6 host without brackets when given
	// "[::1]:8080", so no bracket stripping is needed for that case.
	// Handle the edge case where the input had no port (rare but safe).
	host = strings.Trim(host, "[]")
	if host == "127.0.0.1" || host == "::1" || host == "localhost" {
		return false
	}
	return true
}

// checkTrustedProxyMisconfigured is the production hard-fail counterpart to
// warnIfTrustedProxyMisconfigured. When prod is true and the panel binds to
// a public (non-loopback) address with no trusted-proxy CIDRs configured,
// it returns ErrTrustedProxyMisconfiguredProd so the caller (New()) can
// fail the boot outright rather than starting with a silently-broken
// brute-force defense (S-06 / fleet-wide lockout collapse). In dev
// (prod == false) this always returns nil — the WARN emitted by
// warnIfTrustedProxyMisconfigured is the only signal there, matching the
// ErrInsecureDBDSN (warn+opt-in) vs ErrInsecureDBDSNProd (hard fail, no
// opt-in) split in controlplane/config/storage.go.
func checkTrustedProxyMisconfigured(bindAddr string, trustedCIDRs []*net.IPNet, prod bool) error {
	if !prod {
		return nil
	}
	if len(trustedCIDRs) > 0 {
		return nil
	}
	if !bindAddrIsPublic(bindAddr) {
		return nil
	}
	return ErrTrustedProxyMisconfiguredProd
}

// warnIfTrustedProxyMisconfigured emits a single WARN at startup when the
// operator binds the panel to a non-loopback address but has not listed
// any trusted-proxy CIDRs. In that configuration X-Forwarded-For is
// ignored, every request keys to the proxy IP, and rate-limit buckets the
// entire fleet as one client (S-06).
//
// This is the dev-mode signal only. In production the same condition is a
// hard boot failure — see checkTrustedProxyMisconfigured.
func warnIfTrustedProxyMisconfigured(logger *slog.Logger, bindAddr string, trustedCIDRs []*net.IPNet) {
	if logger == nil {
		return
	}
	if len(trustedCIDRs) > 0 {
		return
	}
	if !bindAddrIsPublic(bindAddr) {
		return
	}
	logger.Warn(
		"trusted_proxy_cidrs is empty while bind is non-loopback; X-Forwarded-For/Proto headers will be ignored, rate limits will bucket the fleet as one client",
		slog.String("bind_addr", bindAddr),
		slog.String("alert", "trusted_proxy_misconfigured"),
		slog.String("remediation", "set PANVEX_TRUSTED_PROXY_CIDRS to your reverse-proxy/CNI subnets"),
	)
}

// remoteAddrIsTrustedProxy reports whether the TCP peer on r (r.RemoteAddr)
// is a loopback address or falls inside one of the configured trusted-proxy
// CIDRs. This gates trust for any hop-attributable header (X-Forwarded-For,
// X-Forwarded-Proto, X-Forwarded-Host, X-Real-IP, ...) so an arbitrary
// untrusted client cannot spoof the deployment topology — e.g. by setting
// `X-Forwarded-Proto: https` over plain-HTTP to trick the server into
// marking the session cookie Secure (finding DF-2 / P2-SEC-04).
func remoteAddrIsTrustedProxy(r *http.Request, trustedCIDRs []*net.IPNet) bool {
	if r == nil {
		return false
	}
	peerHost, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		peerHost = strings.TrimSpace(r.RemoteAddr)
	}
	ip := net.ParseIP(peerHost)
	return peerIsTrusted(ip, trustedCIDRs)
}
