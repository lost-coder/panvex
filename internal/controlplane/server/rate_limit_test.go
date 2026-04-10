package server

import (
	"net"
	"net/http/httptest"
	"testing"
)

func TestRequestClientRateLimitKeyIgnoresForwardedForFromNonLoopbackPeer(t *testing.T) {
	s := &Server{}
	request := httptest.NewRequest("POST", "/api/auth/login", nil)
	request.RemoteAddr = "198.51.100.10:4321"
	request.Header.Set("X-Forwarded-For", "203.0.113.20")

	key := s.requestClientRateLimitKey(request)
	if key != "198.51.100.10" {
		t.Fatalf("requestClientRateLimitKey() = %q, want %q", key, "198.51.100.10")
	}
}

func TestRequestClientRateLimitKeyUsesForwardedForFromLoopbackProxy(t *testing.T) {
	s := &Server{}
	request := httptest.NewRequest("POST", "/api/auth/login", nil)
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
	request := httptest.NewRequest("POST", "/api/auth/login", nil)
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
	request := httptest.NewRequest("POST", "/api/auth/login", nil)
	request.RemoteAddr = "172.18.0.2:8080"
	request.Header.Set("X-Forwarded-For", "203.0.113.50")

	key := s.requestClientRateLimitKey(request)
	if key != "172.18.0.2" {
		t.Fatalf("requestClientRateLimitKey() = %q, want %q", key, "172.18.0.2")
	}
}
