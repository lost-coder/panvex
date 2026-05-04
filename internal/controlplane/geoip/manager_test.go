package geoip_test

import (
	"net"
	"path/filepath"
	"sync"
	"testing"

	"github.com/lost-coder/panvex/internal/controlplane/geoip"
)

func fixture(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join("testdata", name)
}

func TestManagerEmptyLookupReturnsZero(t *testing.T) {
	m := geoip.NewManager(nil)
	defer m.Close()

	city, ok := m.LookupCity(net.ParseIP("8.8.8.8"))
	if ok {
		t.Errorf("LookupCity on empty manager: ok=true, want false; result=%+v", city)
	}
	asn, ok := m.LookupASN(net.ParseIP("8.8.8.8"))
	if ok {
		t.Errorf("LookupASN on empty manager: ok=true, want false; result=%+v", asn)
	}
}

func TestManagerReloadAndLookup(t *testing.T) {
	m := geoip.NewManager(nil)
	defer m.Close()

	if err := m.Reload(fixture(t, "GeoLite2-City-Test.mmdb"), fixture(t, "GeoLite2-ASN-Test.mmdb")); err != nil {
		t.Fatalf("Reload: %v", err)
	}

	// MaxMind's test fixtures contain documented sample IPs.
	// 81.2.69.142 has city "London" / country GB in GeoLite2-City-Test.
	city, ok := m.LookupCity(net.ParseIP("81.2.69.142"))
	if !ok {
		t.Fatalf("LookupCity ok=false")
	}
	if city.CountryCode != "GB" {
		t.Errorf("CountryCode = %q, want GB", city.CountryCode)
	}
	if city.City == "" {
		t.Errorf("City = empty, want non-empty")
	}
}

func TestManagerReloadIsAtomic(t *testing.T) {
	m := geoip.NewManager(nil)
	defer m.Close()

	if err := m.Reload(fixture(t, "GeoLite2-City-Test.mmdb"), fixture(t, "GeoLite2-ASN-Test.mmdb")); err != nil {
		t.Fatalf("first Reload: %v", err)
	}

	// Reload to the same files in parallel with a flood of lookups.
	// With proper RWMutex usage neither call should panic / race.
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = m.LookupCity(net.ParseIP("81.2.69.142"))
		}()
	}
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = m.Reload(fixture(t, "GeoLite2-City-Test.mmdb"), fixture(t, "GeoLite2-ASN-Test.mmdb"))
		}()
	}
	wg.Wait()
}

func TestManagerCloseIsIdempotent(t *testing.T) {
	m := geoip.NewManager(nil)
	if err := m.Reload(fixture(t, "GeoLite2-City-Test.mmdb"), fixture(t, "GeoLite2-ASN-Test.mmdb")); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if err := m.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := m.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
}

func TestManagerSkipsPrivateIP(t *testing.T) {
	m := geoip.NewManager(nil)
	defer m.Close()

	if err := m.Reload(fixture(t, "GeoLite2-City-Test.mmdb"), fixture(t, "GeoLite2-ASN-Test.mmdb")); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if _, ok := m.LookupCity(net.ParseIP("10.0.0.1")); ok {
		t.Error("private IP returned ok=true, expected false")
	}
}
