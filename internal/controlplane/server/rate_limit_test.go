package server

import (
	"net/http/httptest"
	"testing"
)

func TestRequestClientRateLimitKeyIgnoresForwardedForFromNonLoopbackPeer(t *testing.T) {
	request := httptest.NewRequest("POST", "/api/auth/login", nil)
	request.RemoteAddr = "198.51.100.10:4321"
	request.Header.Set("X-Forwarded-For", "203.0.113.20")

	key := requestClientRateLimitKey(request)
	if key != "198.51.100.10" {
		t.Fatalf("requestClientRateLimitKey() = %q, want %q", key, "198.51.100.10")
	}
}

func TestRequestClientRateLimitKeyUsesForwardedForFromLoopbackProxy(t *testing.T) {
	request := httptest.NewRequest("POST", "/api/auth/login", nil)
	request.RemoteAddr = "127.0.0.1:8080"
	request.Header.Set("X-Forwarded-For", "203.0.113.20")

	key := requestClientRateLimitKey(request)
	if key != "203.0.113.20" {
		t.Fatalf("requestClientRateLimitKey() = %q, want %q", key, "203.0.113.20")
	}
}
