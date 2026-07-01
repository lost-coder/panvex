package updates

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
// is enforced at dial time by GeoIPDownloadClient's egress guard, which is
// redirect- and DNS-rebinding-safe.
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

// isBlockedIP reports whether addr is an internal/non-public destination that
// GeoIP downloads must never reach: loopback, RFC1918/RFC4193 private,
// link-local (incl. 169.254.169.254 cloud metadata), multicast, or
// unspecified. Public global unicast is allowed.
func isBlockedIP(addr netip.Addr) bool {
	if !addr.IsValid() {
		return true
	}
	addr = addr.Unmap()
	return addr.IsLoopback() ||
		addr.IsPrivate() ||
		addr.IsLinkLocalUnicast() ||
		addr.IsLinkLocalMulticast() ||
		addr.IsMulticast() ||
		addr.IsUnspecified() ||
		addr.IsInterfaceLocalMulticast()
}

// checkDialAddress rejects a resolved "ip:port" targeting a non-public
// address. It runs from net.Dialer.Control after DNS resolution and before
// connect, so it sees the actual IP the socket would reach — closing the
// TOCTOU/DNS-rebinding gap a URL-time check would leave open.
func checkDialAddress(address string) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("parse dial address %q: %w", address, err)
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return fmt.Errorf("dial address %q is not a literal IP: %w", host, err)
	}
	if isBlockedIP(addr) {
		return fmt.Errorf("refusing to connect to non-public address %s", addr)
	}
	return nil
}

// GeoIPDownloadClient returns an *http.Client that permits any public https
// host but blocks internal destinations at dial time, on every redirect hop.
func GeoIPDownloadClient() *http.Client {
	dialer := &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
		Control: func(_, address string, _ syscall.RawConn) error {
			return checkDialAddress(address)
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
