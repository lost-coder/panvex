package geoip_test

import (
	"net"
	"testing"

	"github.com/lost-coder/panvex/internal/controlplane/geoip"
)

func TestShouldLookup(t *testing.T) {
	cases := []struct {
		name string
		ip   string
		want bool
	}{
		{"public ipv4", "8.8.8.8", true},
		{"public ipv6", "2001:db8::1", true},
		{"loopback", "127.0.0.1", false},
		{"private 10/8", "10.0.0.1", false},
		{"private 192.168/16", "192.168.1.1", false},
		{"link-local", "169.254.0.1", false},
		{"unspecified", "0.0.0.0", false},
		{"ipv6 loopback", "::1", false},
		{"ipv6 ULA", "fd00::1", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ip := net.ParseIP(tc.ip)
			if ip == nil {
				t.Fatalf("ParseIP(%q) returned nil", tc.ip)
			}
			if got := geoip.ShouldLookup(ip); got != tc.want {
				t.Errorf("ShouldLookup(%q) = %v, want %v", tc.ip, got, tc.want)
			}
		})
	}
}

func TestASNResultDisplay(t *testing.T) {
	cases := []struct {
		name string
		asn  uint
		org  string
		want string
	}{
		{"both fields", 13335, "Cloudflare, Inc.", "AS13335 Cloudflare, Inc."},
		{"asn only", 13335, "", "AS13335"},
		{"org only", 0, "Cloudflare", "Cloudflare"},
		{"empty", 0, "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := geoip.ASNResult{Number: tc.asn, Organization: tc.org}
			if got := r.Display(); got != tc.want {
				t.Errorf("Display() = %q, want %q", got, tc.want)
			}
		})
	}
}
