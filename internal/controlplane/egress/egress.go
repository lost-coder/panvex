// Package egress owns outbound-HTTP safety for the control-plane: which
// destinations the panel is allowed to reach and the clients that enforce it.
//
// The guard lives at Dialer.Control — after DNS resolution, before connect —
// so it sees the IP the socket would actually reach. A URL-time or
// resolve-time check cannot do that: the name can resolve to a public address
// when checked and a private one when dialed (DNS rebinding), and every
// redirect hop reopens the same window.
package egress

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"syscall"
	"time"
)

// CheckGeoIPURL validates a GeoIP source URL. Unlike self-update, GeoIP
// sources are legitimately diverse (MaxMind, mirrors, private CDNs), so
// there is no host allow-list here — only https is required. SSRF protection
// is enforced at dial time by GeoIPDownloadClient's guard.
func CheckGeoIPURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("parse url: %w", err)
	}
	if u.Scheme != "https" {
		return fmt.Errorf("url %q: only https is allowed", raw)
	}
	if u.Hostname() == "" {
		return fmt.Errorf("url %q: missing host", raw)
	}
	return nil
}

// extraBlockedPrefixes covers internal ranges the netip predicates miss:
// CGNAT shared address space (RFC6598), commonly carrier/cloud-internal.
var extraBlockedPrefixes = []netip.Prefix{
	netip.MustParsePrefix("100.64.0.0/10"),
}

// IsBlocked reports whether addr is an internal/non-public destination the
// panel must never reach: loopback, RFC1918/RFC4193 private, link-local
// (incl. 169.254.169.254 cloud metadata), multicast, unspecified, or CGNAT
// shared address space (RFC6598). Public global unicast is allowed.
//
// This is the single predicate behind every egress decision — the dial-time
// guard below and the webhook worker's preflight both call it, so a range
// added here is blocked everywhere.
func IsBlocked(addr netip.Addr) bool {
	if !addr.IsValid() {
		return true
	}
	addr = addr.Unmap()
	if addr.IsLoopback() ||
		addr.IsPrivate() ||
		addr.IsLinkLocalUnicast() ||
		addr.IsLinkLocalMulticast() ||
		addr.IsMulticast() ||
		addr.IsUnspecified() ||
		addr.IsInterfaceLocalMulticast() {
		return true
	}
	for _, p := range extraBlockedPrefixes {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}

// CheckDialAddress rejects a resolved "ip:port" targeting a non-public
// address. Wired into net.Dialer.Control.
func CheckDialAddress(address string) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("parse dial address %q: %w", address, err)
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return fmt.Errorf("dial address %q is not a literal IP: %w", host, err)
	}
	if IsBlocked(addr) {
		return fmt.Errorf("refusing to connect to non-public address %s", addr)
	}
	return nil
}

// GuardedDialer is a net.Dialer that refuses to connect to any non-public
// address. dialTimeout bounds the connect itself.
func GuardedDialer(dialTimeout time.Duration) *net.Dialer {
	return &net.Dialer{
		Timeout:   dialTimeout,
		KeepAlive: 30 * time.Second,
		Control: func(_, address string, _ syscall.RawConn) error {
			return CheckDialAddress(address)
		},
	}
}

// GuardedClient returns an *http.Client whose every connection — including
// each redirect hop — goes through the egress guard. Used by the webhook
// worker for endpoints that have not opted into private destinations.
func GuardedClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			DialContext:         GuardedDialer(10 * time.Second).DialContext,
			ForceAttemptHTTP2:   true,
			TLSHandshakeTimeout: 10 * time.Second,
		},
	}
}

// GeoIPDownloadClient returns an *http.Client that permits any public https
// host but blocks internal destinations at dial time, on every redirect hop.
// The long timeout fits a multi-hundred-MB database download.
func GeoIPDownloadClient() *http.Client {
	dialer := &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
		Control: func(_, address string, _ syscall.RawConn) error {
			return CheckDialAddress(address)
		},
	}
	return &http.Client{
		Timeout: 10 * time.Minute,
		Transport: &http.Transport{
			DialContext:         dialer.DialContext,
			ForceAttemptHTTP2:   true,
			TLSHandshakeTimeout: 10 * time.Second,
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return errors.New("stopped after 10 redirects")
			}
			if req.URL.Scheme != "https" {
				return fmt.Errorf("redirect to non-https URL blocked: %q", req.URL.String())
			}
			return nil
		},
	}
}
