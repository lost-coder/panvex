package server

import (
	"net/http/httptest"
	"slices"
	"testing"
)

func TestWSOriginPatternsProductionStrict(t *testing.T) {
	t.Setenv(EnvWSDevLoopback, "")

	r := httptest.NewRequest("GET", "/api/v1/events", nil)
	r.Host = "panvex.example:443"
	r.RemoteAddr = "127.0.0.1:54321"

	server := &Server{}
	got := server.wsOriginPatterns(r)

	want := []string{"panvex.example:443"}
	if !slices.Equal(got, want) {
		t.Fatalf("wsOriginPatterns() = %v, want %v", got, want)
	}
}

func TestWSOriginPatternsDevLoopbackOptIn(t *testing.T) {
	t.Setenv(EnvWSDevLoopback, "1")

	r := httptest.NewRequest("GET", "/api/v1/events", nil)
	r.Host = "localhost:8080"
	r.RemoteAddr = "127.0.0.1:54321"

	server := &Server{}
	got := server.wsOriginPatterns(r)

	if len(got) < 2 {
		t.Fatalf("expected loopback wildcards under dev opt-in, got %v", got)
	}
	if got[0] != "localhost:8080" {
		t.Fatalf("first pattern = %q, want exact host match", got[0])
	}
}

func TestWSOriginPatternsDevLoopbackSkippedForNonLoopback(t *testing.T) {
	t.Setenv(EnvWSDevLoopback, "1")

	r := httptest.NewRequest("GET", "/api/v1/events", nil)
	r.Host = "localhost:8080"
	r.RemoteAddr = "203.0.113.7:54321"

	server := &Server{}
	got := server.wsOriginPatterns(r)

	want := []string{"localhost:8080"}
	if !slices.Equal(got, want) {
		t.Fatalf("non-loopback client should not get wildcard: got %v, want %v", got, want)
	}
}
