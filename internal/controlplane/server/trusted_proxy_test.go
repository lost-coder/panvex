package server

import (
	"bytes"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
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
	r := httptest.NewRequestWithContext(t.Context(),http.MethodGet, "/", nil)
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
	r := httptest.NewRequestWithContext(t.Context(),http.MethodGet, "/", nil)
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
	r := httptest.NewRequestWithContext(t.Context(),http.MethodGet, "/", nil)
	r.RemoteAddr = "10.0.0.1:45678"
	r.Header.Set("X-Forwarded-For", "1.2.3.4, 10.0.0.2")

	trusted := []*net.IPNet{mustCIDR(t, "10.0.0.0/24")}
	ip := resolveTrustedClientIP(r, trusted)
	if got := ip.String(); got != "1.2.3.4" {
		t.Fatalf("got %q, want 1.2.3.4", got)
	}
}

func TestResolveTrustedClientIP_LoopbackPeer_AlwaysTrusted(t *testing.T) {
	r := httptest.NewRequestWithContext(t.Context(),http.MethodGet, "/", nil)
	r.RemoteAddr = "127.0.0.1:45678"
	r.Header.Set("X-Forwarded-For", "1.2.3.4")

	ip := resolveTrustedClientIP(r, nil)
	if got := ip.String(); got != "1.2.3.4" {
		t.Fatalf("got %q, want 1.2.3.4 (loopback peer trusted)", got)
	}
}

func TestResolveTrustedClientIP_IPv6_BracketedPortStripped(t *testing.T) {
	r := httptest.NewRequestWithContext(t.Context(),http.MethodGet, "/", nil)
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
	r := httptest.NewRequestWithContext(t.Context(),http.MethodGet, "/", nil)
	r.RemoteAddr = "10.0.0.1:80"
	r.Header.Set("X-Forwarded-For", "10.0.0.3, 10.0.0.2")
	trusted := []*net.IPNet{mustCIDR(t, "10.0.0.0/24")}

	ip := resolveTrustedClientIP(r, trusted)
	if got := ip.String(); got != "10.0.0.3" {
		t.Fatalf("got %q, want 10.0.0.3 (leftmost when chain all trusted)", got)
	}
}

func TestResolveTrustedClientIP_MalformedXFF(t *testing.T) {
	r := httptest.NewRequestWithContext(t.Context(),http.MethodGet, "/", nil)
	r.RemoteAddr = "127.0.0.1:80"
	r.Header.Set("X-Forwarded-For", "not-an-ip, 2001:db8::1")

	ip := resolveTrustedClientIP(r, nil)
	if got := ip.String(); got != "2001:db8::1" {
		t.Fatalf("got %q, want 2001:db8::1 (valid hop past malformed entry)", got)
	}
}

func TestResolveTrustedClientIP_EmptyRemoteAddr(t *testing.T) {
	r := httptest.NewRequestWithContext(t.Context(),http.MethodGet, "/", nil)
	r.RemoteAddr = ""

	ip := resolveTrustedClientIP(r, nil)
	if ip != nil {
		t.Fatalf("got %v, want nil for empty RemoteAddr", ip)
	}
}

func TestWarnIfTrustedProxyMisconfigured(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		bind        string
		cidrs       []*net.IPNet
		wantWarning bool
	}{
		{"loopback bind, empty CIDR", "127.0.0.1:8080", nil, false},
		{"public bind, empty CIDR", "0.0.0.0:8080", nil, true},
		{"public bind, configured CIDR", "0.0.0.0:8080", []*net.IPNet{mustCIDR(t, "10.0.0.0/8")}, false},
		{"unspecified bind, empty CIDR", ":8080", nil, true},
		{"localhost name, empty CIDR", "localhost:8080", nil, false},
		{"ipv6 loopback, empty CIDR", "[::1]:8080", nil, false},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer
			logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))
			warnIfTrustedProxyMisconfigured(logger, tt.bind, tt.cidrs)
			got := strings.Contains(buf.String(), "trusted_proxy_cidrs is empty")
			if got != tt.wantWarning {
				t.Fatalf("warning emitted = %v, want %v\nlog: %s", got, tt.wantWarning, buf.String())
			}
		})
	}
}

// TestCheckTrustedProxyMisconfigured pins the prod hard-fail: a public
// (non-loopback) HTTP bind with empty TrustedProxyCIDRs must be rejected
// outright when PANVEX_ENV=production (mirrors the ErrInsecure*Prod
// fail-loud idiom in controlplane/config/storage.go), while the identical
// configuration in dev only warns (handled separately by
// warnIfTrustedProxyMisconfigured) and must NOT fail here.
//
// Review fix: the hard-fail must not block a legitimate direct-exposure
// deployment (no reverse proxy at all) that opts in via
// PANVEX_ALLOW_DIRECT_EXPOSURE=1 — see the "prod, public bind, empty CIDR,
// direct exposure allowed" row.
func TestCheckTrustedProxyMisconfigured(t *testing.T) {
	tests := []struct {
		name                string
		bind                string
		cidrs               []*net.IPNet
		prod                bool
		allowDirectExposure bool
		wantErr             bool
	}{
		{"prod, public bind, empty CIDR", "0.0.0.0:8080", nil, true, false, true},
		{"prod, unspecified bind, empty CIDR", ":8080", nil, true, false, true},
		{"prod, public bind, configured CIDR", "0.0.0.0:8080", []*net.IPNet{mustCIDR(t, "10.0.0.0/8")}, true, false, false},
		{"prod, loopback bind, empty CIDR", "127.0.0.1:8080", nil, true, false, false},
		{"prod, ipv6 loopback bind, empty CIDR", "[::1]:8080", nil, true, false, false},
		{"dev, public bind, empty CIDR", "0.0.0.0:8080", nil, false, false, false},
		{"prod, public bind, empty CIDR, direct exposure allowed", "0.0.0.0:8080", nil, true, true, false},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			err := checkTrustedProxyMisconfigured(tt.bind, tt.cidrs, tt.prod, tt.allowDirectExposure)
			if tt.wantErr && !errors.Is(err, ErrTrustedProxyMisconfiguredProd) {
				t.Fatalf("checkTrustedProxyMisconfigured() error = %v, want ErrTrustedProxyMisconfiguredProd", err)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("checkTrustedProxyMisconfigured() error = %v, want nil", err)
			}
		})
	}
}

// TestDirectExposureAllowed pins the PANVEX_ALLOW_DIRECT_EXPOSURE parsing
// idiom: accepts "1"/"true" (and other strconv.ParseBool truthy forms),
// case-insensitively, and treats unset/empty/invalid/false values as "not
// allowed" rather than erroring.
func TestDirectExposureAllowed(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  bool
	}{
		{"unset/empty string", "", false},
		{"1", "1", true},
		{"true", "true", true},
		{"TRUE", "TRUE", true},
		{"0", "0", false},
		{"false", "false", false},
		{"garbage", "not-a-bool", false},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(EnvAllowDirectExposure, tt.value)
			if got := directExposureAllowed(); got != tt.want {
				t.Fatalf("directExposureAllowed() = %v, want %v (value=%q)", got, tt.want, tt.value)
			}
		})
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
