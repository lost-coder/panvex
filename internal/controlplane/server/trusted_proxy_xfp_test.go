package server

import (
	"crypto/tls"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestRemoteAddrIsTrustedProxy exercises the predicate directly across
// loopback, configured CIDR, and unrelated-peer cases.
func TestRemoteAddrIsTrustedProxy(t *testing.T) {
	_, lanCIDR, err := net.ParseCIDR("10.0.0.0/8")
	if err != nil {
		t.Fatalf("net.ParseCIDR() error = %v", err)
	}
	cidrs := []*net.IPNet{lanCIDR}

	tests := []struct {
		name       string
		remoteAddr string
		want       bool
	}{
		{"loopback_ipv4_is_trusted", "127.0.0.1:4242", true},
		{"loopback_ipv6_is_trusted", "[::1]:4242", true},
		{"inside_configured_cidr_is_trusted", "10.2.3.4:8080", true},
		{"outside_configured_cidr_is_untrusted", "203.0.113.7:8080", false},
		{"bare_host_without_port_is_parsed", "127.0.0.1", true},
		{"invalid_remote_addr_is_untrusted", "not-an-ip", false},
		{"empty_remote_addr_is_untrusted", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			r.RemoteAddr = tt.remoteAddr
			if got := remoteAddrIsTrustedProxy(r, cidrs); got != tt.want {
				t.Fatalf("remoteAddrIsTrustedProxy(%q) = %v, want %v", tt.remoteAddr, got, tt.want)
			}
		})
	}

	t.Run("nil_request_is_untrusted", func(t *testing.T) {
		if remoteAddrIsTrustedProxy(nil, cidrs) {
			t.Fatal("remoteAddrIsTrustedProxy(nil) = true, want false")
		}
	})
}

// newServerForCookieTest builds a bare Server with just enough wiring to
// evaluate sessionCookieSecure. We don't need the full auth/storage stack —
// the method only consults r.TLS, headers, trustedProxyCIDRs, panelRuntime,
// and a snapshot of panelSettings.
func newServerForCookieTest(t *testing.T, trusted []*net.IPNet, tlsMode string, httpPublicURL string) *Server {
	t.Helper()
	s := &Server{
		trustedProxyCIDRs: trusted,
		panelRuntime: PanelRuntime{
			TLSMode: tlsMode,
		},
	}
	s.panelSettings = PanelSettings{HTTPPublicURL: httpPublicURL}
	return s
}

// TestSessionCookieSecureTLSAndTrustMatrix covers the four cases called out
// by P2-SEC-04 and ensures direct-TLS always wins regardless of XFP.
func TestSessionCookieSecureTLSAndTrustMatrix(t *testing.T) {
	_, lanCIDR, err := net.ParseCIDR("10.0.0.0/8")
	if err != nil {
		t.Fatalf("net.ParseCIDR() error = %v", err)
	}
	trusted := []*net.IPNet{lanCIDR}

	// Use TLSMode="proxy" and empty HTTPPublicURL so the base (no-XFP,
	// no-r.TLS) result is false — the test then isolates the effect of XFP
	// under trusted vs untrusted peers.
	s := newServerForCookieTest(t, trusted, "proxy", "")

	newReq := func(remoteAddr string, xfp string, withTLS bool) *http.Request {
		r := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader("{}"))
		r.RemoteAddr = remoteAddr
		if xfp != "" {
			r.Header.Set("X-Forwarded-Proto", xfp)
		}
		if withTLS {
			// Any non-nil ConnectionState marks the connection as TLS for the
			// purposes of r.TLS != nil.
			r.TLS = &tls.ConnectionState{}
		}
		return r
	}

	t.Run("direct_tls_untrusted_peer_xfp_http_still_secure", func(t *testing.T) {
		r := newReq("203.0.113.7:55555", "http", true)
		if !s.sessionCookieSecure(r) {
			t.Fatal("r.TLS!=nil must force Secure=true regardless of XFP")
		}
	})

	t.Run("direct_tls_untrusted_peer_no_xfp_still_secure", func(t *testing.T) {
		r := newReq("203.0.113.7:55555", "", true)
		if !s.sessionCookieSecure(r) {
			t.Fatal("r.TLS!=nil must force Secure=true")
		}
	})

	t.Run("plain_http_untrusted_peer_xfp_https_is_not_secure_DF2_fix", func(t *testing.T) {
		r := newReq("203.0.113.7:55555", "https", false)
		if s.sessionCookieSecure(r) {
			t.Fatal("untrusted peer spoofed XFP=https must NOT result in Secure=true (DF-2 fix)")
		}
	})

	t.Run("plain_http_trusted_peer_xfp_https_is_secure_backcompat", func(t *testing.T) {
		r := newReq("10.1.2.3:55555", "https", false)
		if !s.sessionCookieSecure(r) {
			t.Fatal("trusted proxy forwarding XFP=https must yield Secure=true")
		}
	})

	t.Run("plain_http_loopback_peer_xfp_https_is_secure_backcompat", func(t *testing.T) {
		r := newReq("127.0.0.1:55555", "https", false)
		if !s.sessionCookieSecure(r) {
			t.Fatal("loopback proxy forwarding XFP=https must yield Secure=true")
		}
	})

	t.Run("plain_http_trusted_peer_xfp_http_is_not_secure", func(t *testing.T) {
		r := newReq("10.1.2.3:55555", "http", false)
		if s.sessionCookieSecure(r) {
			t.Fatal("trusted proxy forwarding XFP=http must yield Secure=false")
		}
	})

	t.Run("plain_http_trusted_peer_xfp_comma_list_first_hop_https", func(t *testing.T) {
		// XFP can arrive as a comma-delimited list; the leftmost (original)
		// hop is the authoritative one.
		r := newReq("10.1.2.3:55555", "https, http", false)
		if !s.sessionCookieSecure(r) {
			t.Fatal("comma-separated XFP with leading https must yield Secure=true")
		}
	})
}

// TestSessionCookieSecureFallsBackToPanelTLSModeAndPublicURL verifies the
// existing fallback chain (TLSMode==direct, HTTPPublicURL scheme) is still
// honoured after the trust gate is added — these signals are static
// deployment state and cannot be spoofed per-request.
func TestSessionCookieSecureFallsBackToPanelTLSModeAndPublicURL(t *testing.T) {
	_, lanCIDR, _ := net.ParseCIDR("10.0.0.0/8")

	t.Run("tls_mode_direct_is_secure_even_without_xfp", func(t *testing.T) {
		s := newServerForCookieTest(t, []*net.IPNet{lanCIDR}, panelTLSModeDirect, "")
		r := httptest.NewRequest(http.MethodPost, "/", nil)
		r.RemoteAddr = "203.0.113.7:55555"
		if !s.sessionCookieSecure(r) {
			t.Fatal("TLSMode=direct must yield Secure=true")
		}
	})

	t.Run("https_public_url_is_secure", func(t *testing.T) {
		s := newServerForCookieTest(t, []*net.IPNet{lanCIDR}, "proxy", "https://panel.example.com")
		r := httptest.NewRequest(http.MethodPost, "/", nil)
		r.RemoteAddr = "203.0.113.7:55555"
		if !s.sessionCookieSecure(r) {
			t.Fatal("HTTPPublicURL=https:// must yield Secure=true")
		}
	})

	t.Run("http_public_url_is_not_secure", func(t *testing.T) {
		s := newServerForCookieTest(t, []*net.IPNet{lanCIDR}, "proxy", "http://panel.example.com")
		r := httptest.NewRequest(http.MethodPost, "/", nil)
		r.RemoteAddr = "203.0.113.7:55555"
		if s.sessionCookieSecure(r) {
			t.Fatal("HTTPPublicURL=http:// must yield Secure=false")
		}
	})
}

// TestTrustedForwardedProtoGating validates the s.trustedForwardedProto
// helper that threads the same trust gate to buildAgentPublicURL callers.
func TestTrustedForwardedProtoGating(t *testing.T) {
	_, lanCIDR, _ := net.ParseCIDR("10.0.0.0/8")
	s := &Server{trustedProxyCIDRs: []*net.IPNet{lanCIDR}}

	tests := []struct {
		name       string
		remoteAddr string
		xfp        string
		want       string
	}{
		{"untrusted_peer_xfp_dropped", "203.0.113.7:55555", "https", ""},
		{"trusted_peer_xfp_pass_through", "10.9.9.9:55555", "https", "https"},
		{"loopback_peer_xfp_pass_through", "127.0.0.1:55555", "http", "http"},
		{"trusted_peer_empty_xfp_empty", "10.9.9.9:55555", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			r.RemoteAddr = tt.remoteAddr
			if tt.xfp != "" {
				r.Header.Set("X-Forwarded-Proto", tt.xfp)
			}
			if got := s.trustedForwardedProto(r); got != tt.want {
				t.Fatalf("trustedForwardedProto() = %q, want %q", got, tt.want)
			}
		})
	}
}
