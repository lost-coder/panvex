package server

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

func parseCIDRs(cidrs []string) []*net.IPNet {
	var result []*net.IPNet
	for _, c := range cidrs {
		_, ipNet, err := net.ParseCIDR(c)
		if err == nil {
			result = append(result, ipNet)
		}
	}
	return result
}

func TestIPWhitelistAllowsMatchingIP(t *testing.T) {
	allowed := parseCIDRs([]string{"10.0.0.0/8"})
	handler := ipWhitelistMiddleware(allowed, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.1.2.3:12345"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestIPWhitelistBlocksNonMatchingIP(t *testing.T) {
	allowed := parseCIDRs([]string{"10.0.0.0/8"})
	handler := ipWhitelistMiddleware(allowed, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "203.0.113.5:12345"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestIPWhitelistEmptyListAllowsAll(t *testing.T) {
	handler := ipWhitelistMiddleware(nil, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "203.0.113.5:12345"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d — empty list should allow all", rec.Code, http.StatusOK)
	}
}

// TestIPWhitelistRespectsXForwardedFor_LoopbackPeer asserts that XFF is honoured
// when the peer is loopback — our default "trusted proxy" assumption for local
// reverse-proxy topologies.
func TestIPWhitelistRespectsXForwardedFor_LoopbackPeer(t *testing.T) {
	allowed := parseCIDRs([]string{"192.168.1.0/24"})
	handler := ipWhitelistMiddleware(allowed, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	req.Header.Set("X-Forwarded-For", "192.168.1.50")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

// TestIPWhitelistRejectsSpoofedXFF asserts that an attacker who forges XFF
// from an untrusted peer cannot bypass the whitelist. This is the SEC-04 / C3
// finding: the old leftmost-XFF implementation let this through.
func TestIPWhitelistRejectsSpoofedXFF(t *testing.T) {
	allowed := parseCIDRs([]string{"192.168.1.0/24"})
	handler := ipWhitelistMiddleware(allowed, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "203.0.113.5:12345" // untrusted, not in 192.168.1.0/24
	req.Header.Set("X-Forwarded-For", "192.168.1.50")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d — spoofed XFF from untrusted peer must not bypass whitelist", rec.Code, http.StatusForbidden)
	}
}

func TestIPWhitelistTrustedProxyChain(t *testing.T) {
	// Configured trusted proxy: 10.0.0.0/24. Peer is 10.0.0.1 (trusted);
	// real client 192.168.1.50 was appended leftmost of the chain.
	allowed := parseCIDRs([]string{"192.168.1.0/24"})
	trusted := parseCIDRs([]string{"10.0.0.0/24"})
	handler := ipWhitelistMiddleware(allowed, trusted)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.1:80"
	req.Header.Set("X-Forwarded-For", "192.168.1.50, 10.0.0.2")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestIPWhitelistMultipleCIDRs(t *testing.T) {
	allowed := parseCIDRs([]string{"10.0.0.0/8", "172.16.0.0/12"})
	handler := ipWhitelistMiddleware(allowed, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "172.16.5.10:12345"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}
