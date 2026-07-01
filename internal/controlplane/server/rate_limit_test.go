package server

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequestClientRateLimitKeyIgnoresForwardedForFromNonLoopbackPeer(t *testing.T) {
	s := &Server{}
	request := httptest.NewRequestWithContext(t.Context(),"POST", "/api/auth/login", nil)
	request.RemoteAddr = "198.51.100.10:4321"
	request.Header.Set("X-Forwarded-For", "203.0.113.20")

	key := s.requestClientRateLimitKey(request)
	if key != "198.51.100.10" {
		t.Fatalf("requestClientRateLimitKey() = %q, want %q", key, "198.51.100.10")
	}
}

func TestRequestClientRateLimitKeyUsesForwardedForFromLoopbackProxy(t *testing.T) {
	s := &Server{}
	request := httptest.NewRequestWithContext(t.Context(),"POST", "/api/auth/login", nil)
	request.RemoteAddr = "127.0.0.1:8080"
	request.Header.Set("X-Forwarded-For", "203.0.113.20")

	key := s.requestClientRateLimitKey(request)
	if key != "203.0.113.20" {
		t.Fatalf("requestClientRateLimitKey() = %q, want %q", key, "203.0.113.20")
	}
}

func TestRequestClientRateLimitKeyUsesForwardedForFromTrustedProxyCIDR(t *testing.T) {
	_, cidr, _ := net.ParseCIDR("172.16.0.0/12")
	s := &Server{trustedProxyCIDRs: []*net.IPNet{cidr}}
	request := httptest.NewRequestWithContext(t.Context(),"POST", "/api/auth/login", nil)
	request.RemoteAddr = "172.18.0.2:8080"
	request.Header.Set("X-Forwarded-For", "203.0.113.50")

	key := s.requestClientRateLimitKey(request)
	if key != "203.0.113.50" {
		t.Fatalf("requestClientRateLimitKey() = %q, want %q", key, "203.0.113.50")
	}
}

func TestRequestClientRateLimitKeyIgnoresForwardedForFromUntrustedCIDR(t *testing.T) {
	_, cidr, _ := net.ParseCIDR("10.0.0.0/8")
	s := &Server{trustedProxyCIDRs: []*net.IPNet{cidr}}
	request := httptest.NewRequestWithContext(t.Context(),"POST", "/api/auth/login", nil)
	request.RemoteAddr = "172.18.0.2:8080"
	request.Header.Set("X-Forwarded-For", "203.0.113.50")

	key := s.requestClientRateLimitKey(request)
	if key != "172.18.0.2" {
		t.Fatalf("requestClientRateLimitKey() = %q, want %q", key, "172.18.0.2")
	}
}

// TestRequestClientKey_UsesResolvedClientIP pins requestClientRateLimitKey to
// resolveTrustedClientIP's first-untrusted-hop semantics (trusted_proxy.go),
// the same resolver ipWhitelistMiddleware uses. Before this fix,
// requestClientRateLimitKey used a separate rightmost-XFF-entry heuristic
// (remoteAddrTrustsForwardedFor) — a second, drifted notion of "client
// identity" from the same trusted-proxy configuration.
func TestRequestClientKey_UsesResolvedClientIP(t *testing.T) {
	s := &Server{trustedProxyCIDRs: []*net.IPNet{mustCIDR(t, "10.0.0.0/8")}}
	r := httptest.NewRequestWithContext(t.Context(), "POST", "/auth/login", nil)
	r.RemoteAddr = "10.0.0.1:5555"                 // trusted proxy
	r.Header.Set("X-Forwarded-For", "203.0.113.7") // real client
	got := s.requestClientRateLimitKey(r)
	if got != "203.0.113.7" {
		t.Fatalf("want real client ip key, got %q", got)
	}
}

// TestRequestClientRateLimitKey_DistinctXFFClientsBehindTrustedProxy is the
// direct regression test for the lockout-collapse bug: once a trusted CIDR
// covers the reverse-proxy hop, two different attacker-controlled clients
// (distinguished only by the XFF header the proxy appends) must resolve to
// two different keys, not one shared bucket.
func TestRequestClientRateLimitKey_DistinctXFFClientsBehindTrustedProxy(t *testing.T) {
	s := &Server{trustedProxyCIDRs: []*net.IPNet{mustCIDR(t, "10.0.0.0/8")}}

	newReq := func(xff string) *http.Request {
		r := httptest.NewRequestWithContext(t.Context(), "POST", "/auth/login", nil)
		r.RemoteAddr = "10.0.0.1:5555"
		r.Header.Set("X-Forwarded-For", xff)
		return r
	}

	keyA := s.requestClientRateLimitKey(newReq("203.0.113.7"))
	keyB := s.requestClientRateLimitKey(newReq("198.51.100.9"))

	if keyA == keyB {
		t.Fatalf("distinct clients collapsed into one rate-limit key: %q == %q", keyA, keyB)
	}
	if keyA != "203.0.113.7" || keyB != "198.51.100.9" {
		t.Fatalf("unexpected keys: keyA=%q keyB=%q", keyA, keyB)
	}
}

// TestRequestClientRateLimitKey_SpoofedTrustedRightmostHop pins the case
// where the old rightmost-XFF helper and resolveTrustedClientIP genuinely
// diverge: an attacker behind a trusted proxy appends a *second*,
// spoofed hop that itself falls inside a trusted CIDR (e.g. guessing the
// proxy's own subnet). The old rightmost-entry heuristic
// (remoteAddrTrustsForwardedFor) would key on that spoofed trusted-looking
// address; resolveTrustedClientIP walks right-to-left and skips any hop
// that is itself trusted, correctly landing on the real client IP.
func TestRequestClientRateLimitKey_SpoofedTrustedRightmostHop(t *testing.T) {
	s := &Server{trustedProxyCIDRs: []*net.IPNet{mustCIDR(t, "10.0.0.0/8")}}
	r := httptest.NewRequestWithContext(t.Context(), "POST", "/auth/login", nil)
	r.RemoteAddr = "10.0.0.1:5555" // trusted proxy (peer)
	// Attacker-supplied XFF: real client, then a spoofed hop inside the
	// trusted CIDR appended by the attacker itself (not by the real proxy).
	r.Header.Set("X-Forwarded-For", "203.0.113.7, 10.0.0.99")
	got := s.requestClientRateLimitKey(r)
	if got != "203.0.113.7" {
		t.Fatalf("requestClientRateLimitKey() = %q, want real client ip %q (rightmost-hop heuristic would have returned the spoofed trusted-looking hop)", got, "203.0.113.7")
	}
}
