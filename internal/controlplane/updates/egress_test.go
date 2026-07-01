package updates

import (
	"net/netip"
	"testing"
)

func TestIsBlockedIP(t *testing.T) {
	blocked := []string{
		"127.0.0.1", "::1", // loopback
		"10.0.0.5", "172.16.3.4", "192.168.1.1", // RFC1918
		"fc00::1",         // ULA
		"169.254.169.254", // link-local / cloud metadata
		"fe80::1",         // link-local v6
		"0.0.0.0", "::",   // unspecified
		"224.0.0.1", "ff02::1", // multicast
	}
	for _, s := range blocked {
		if !isBlockedIP(netip.MustParseAddr(s)) {
			t.Errorf("isBlockedIP(%s) = false, want true", s)
		}
	}
	public := []string{"8.8.8.8", "1.1.1.1", "140.82.112.3", "2606:4700:4700::1111"}
	for _, s := range public {
		if isBlockedIP(netip.MustParseAddr(s)) {
			t.Errorf("isBlockedIP(%s) = true, want false (public)", s)
		}
	}
}

func TestCheckDialAddressBlocksInternal(t *testing.T) {
	if err := checkDialAddress("169.254.169.254:80"); err == nil {
		t.Fatal("metadata address accepted by dial guard")
	}
	if err := checkDialAddress("10.0.0.5:443"); err == nil {
		t.Fatal("private address accepted by dial guard")
	}
	if err := checkDialAddress("8.8.8.8:443"); err != nil {
		t.Fatalf("public address rejected by dial guard: %v", err)
	}
}

func TestCheckGeoIPURL(t *testing.T) {
	if err := CheckGeoIPURL("https://download.maxmind.com/GeoLite2-City.mmdb"); err != nil {
		t.Fatalf("public non-GitHub GeoIP host rejected: %v", err)
	}
	if err := CheckGeoIPURL("http://download.maxmind.com/x"); err == nil {
		t.Fatal("http GeoIP URL accepted")
	}
}

// Independence regression: the self-update wildcard must NOT open the GeoIP
// egress guard.
func TestGeoIPGuardIgnoresUpdateWildcard(t *testing.T) {
	t.Setenv("PANVEX_UPDATE_ALLOWED_HOSTS", "*")
	if err := checkDialAddress("10.0.0.5:443"); err == nil {
		t.Fatal("update wildcard leaked into the GeoIP egress guard — SSRF reopened")
	}
}
