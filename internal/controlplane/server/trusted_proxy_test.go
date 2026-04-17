package server

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

func mustCIDR(t *testing.T, s string) *net.IPNet {
	t.Helper()
	_, cidr, err := net.ParseCIDR(s)
	if err != nil {
		t.Fatalf("ParseCIDR(%q) error = %v", s, err)
	}
	return cidr
}

func TestResolveTrustedClientIP_NoXFF_UntrustedPeer(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "203.0.113.10:54321"

	ip := resolveTrustedClientIP(r, nil)
	if got := ip.String(); got != "203.0.113.10" {
		t.Fatalf("got %q, want 203.0.113.10", got)
	}
}

func TestResolveTrustedClientIP_SpoofedXFF_UntrustedPeerIgnored(t *testing.T) {
	// Attacker connects directly (RemoteAddr is their own IP) and spoofs XFF
	// claiming to be an allow-listed address. Because the peer is not a
	// trusted proxy, XFF must be ignored entirely.
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "203.0.113.10:54321"
	r.Header.Set("X-Forwarded-For", "10.0.0.99, 10.0.0.1")

	ip := resolveTrustedClientIP(r, nil)
	if got := ip.String(); got != "203.0.113.10" {
		t.Fatalf("got %q, want 203.0.113.10", got)
	}
}

func TestResolveTrustedClientIP_TrustedProxy_RightmostUntrusted(t *testing.T) {
	// Topology: client 1.2.3.4 -> proxy 10.0.0.2 -> proxy 10.0.0.1 -> server.
	// Both 10.x hops are trusted, so the real client is the leftmost entry.
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "10.0.0.1:45678"
	r.Header.Set("X-Forwarded-For", "1.2.3.4, 10.0.0.2")

	trusted := []*net.IPNet{mustCIDR(t, "10.0.0.0/24")}
	ip := resolveTrustedClientIP(r, trusted)
	if got := ip.String(); got != "1.2.3.4" {
		t.Fatalf("got %q, want 1.2.3.4", got)
	}
}

func TestResolveTrustedClientIP_LoopbackPeer_AlwaysTrusted(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "127.0.0.1:45678"
	r.Header.Set("X-Forwarded-For", "1.2.3.4")

	ip := resolveTrustedClientIP(r, nil)
	if got := ip.String(); got != "1.2.3.4" {
		t.Fatalf("got %q, want 1.2.3.4 (loopback peer trusted)", got)
	}
}

func TestResolveTrustedClientIP_IPv6_BracketedPortStripped(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "[::1]:45678"
	// Rightmost hop is a trusted proxy 10.0.0.2:80 with port; before it, the
	// client 2001:db8::1 was recorded bracketed with port too.
	r.Header.Set("X-Forwarded-For", "[2001:db8::1]:1234, [10.0.0.2]:80")
	trusted := []*net.IPNet{mustCIDR(t, "10.0.0.0/24")}

	ip := resolveTrustedClientIP(r, trusted)
	if got := ip.String(); got != "2001:db8::1" {
		t.Fatalf("got %q, want 2001:db8::1", got)
	}
}

func TestResolveTrustedClientIP_AllTrusted_ReturnsOriginator(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "10.0.0.1:80"
	r.Header.Set("X-Forwarded-For", "10.0.0.3, 10.0.0.2")
	trusted := []*net.IPNet{mustCIDR(t, "10.0.0.0/24")}

	ip := resolveTrustedClientIP(r, trusted)
	if got := ip.String(); got != "10.0.0.3" {
		t.Fatalf("got %q, want 10.0.0.3 (leftmost when chain all trusted)", got)
	}
}

func TestResolveTrustedClientIP_MalformedXFF(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "127.0.0.1:80"
	r.Header.Set("X-Forwarded-For", "not-an-ip, 2001:db8::1")

	ip := resolveTrustedClientIP(r, nil)
	if got := ip.String(); got != "2001:db8::1" {
		t.Fatalf("got %q, want 2001:db8::1 (valid hop past malformed entry)", got)
	}
}

func TestResolveTrustedClientIP_EmptyRemoteAddr(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = ""

	ip := resolveTrustedClientIP(r, nil)
	if ip != nil {
		t.Fatalf("got %v, want nil for empty RemoteAddr", ip)
	}
}

func TestStripPort(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"1.2.3.4", "1.2.3.4"},
		{"1.2.3.4:80", "1.2.3.4"},
		{"2001:db8::1", "2001:db8::1"},
		{"[2001:db8::1]:80", "2001:db8::1"},
		{"[2001:db8::1]", "2001:db8::1"},
	}
	for _, c := range cases {
		if got := stripPort(c.in); got != c.want {
			t.Errorf("stripPort(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
