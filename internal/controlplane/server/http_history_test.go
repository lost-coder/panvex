package server

import (
	"net"
	"path/filepath"
	"testing"
	"time"
)

// TestEnrichIPRowsPopulatesGeoIPFields covers Task 16 of the GeoIP
// plan: when Server.geoip has loaded readers, enrichIPRows must fill
// in CountryCode/CountryName/City/ASN for rows whose IP resolves in
// the DB. Rows whose IP is private/loopback/unparseable, or any row
// at all when the Manager is empty, must be left untouched so the
// JSON shape collapses back to the legacy {ip_address, first_seen,
// last_seen} via `omitempty`.
//
// We exercise the helper directly (rather than seeding
// client_ip_history and round-tripping through the HTTP handler)
// because the helper is the single point of GeoIP integration —
// testing it in isolation keeps the test fast and decoupled from the
// storage / batch-writer pipeline. The handler invocation itself is
// a one-line `s.enrichIPRows(ips)` and is covered structurally by
// `go build ./...` + the existing IP-history handler tests.
func TestEnrichIPRowsPopulatesGeoIPFields(t *testing.T) {
	now := time.Date(2026, time.May, 4, 10, 35, 0, 0, time.UTC)
	t.Setenv("PANVEX_GEOIP_DIR", t.TempDir())
	server := testServerWithSQLite(t, now)

	// Reload the Manager pointed at the MaxMind sample fixtures used
	// by internal/controlplane/geoip/*_test.go. 81.2.69.142 is the
	// canonical "London / GB" record in GeoLite2-City-Test.
	cityFixture := filepath.Join("..", "geoip", "testdata", "GeoLite2-City-Test.mmdb")
	asnFixture := filepath.Join("..", "geoip", "testdata", "GeoLite2-ASN-Test.mmdb")
	if err := server.geoip.Reload(cityFixture, asnFixture); err != nil {
		t.Fatalf("geoip.Reload(%q, %q) error = %v", cityFixture, asnFixture, err)
	}

	rows := []clientIPRow{
		{IPAddress: "81.2.69.142", FirstSeen: now, LastSeen: now},
		{IPAddress: "10.0.0.1", FirstSeen: now, LastSeen: now},     // private — ShouldLookup skips
		{IPAddress: "not-an-ip", FirstSeen: now, LastSeen: now},    // unparseable — guarded
	}
	server.enrichIPRows(rows)

	// Public IP must pick up the City record. The fixture's English
	// City name and a non-empty CountryName are stable across MaxMind
	// test-DB releases; we assert CountryCode == "GB" (the strongest
	// guarantee the upstream README makes) and that City is non-
	// empty so any future fixture reshuffle that drops the city name
	// fails loud rather than silently emitting a half-populated row.
	if got := rows[0].CountryCode; got != "GB" {
		t.Errorf("rows[0].CountryCode = %q, want %q", got, "GB")
	}
	if rows[0].CountryName == "" {
		t.Errorf("rows[0].CountryName = empty, want non-empty")
	}
	if rows[0].City == "" {
		t.Errorf("rows[0].City = empty, want non-empty")
	}

	// Private IP: the Manager's ShouldLookup short-circuits before
	// touching the DB, so all four enrichment fields stay zero.
	if rows[1].CountryCode != "" || rows[1].CountryName != "" || rows[1].City != "" || rows[1].ASN != "" {
		t.Errorf("rows[1] (private IP) enriched: %+v, want all-empty", rows[1])
	}

	// Unparseable IP: net.ParseIP returns nil, the loop continues
	// without touching the row. Same expectation as the private IP.
	if rows[2].CountryCode != "" || rows[2].CountryName != "" || rows[2].City != "" || rows[2].ASN != "" {
		t.Errorf("rows[2] (unparseable IP) enriched: %+v, want all-empty", rows[2])
	}

	// Sanity-check the helper actually used the manager we wired in
	// (defence-in-depth: a future refactor that accidentally swaps
	// the field name or shadows it would otherwise still report
	// success because every row is "untouched"). 81.2.69.142 must
	// also resolve through a direct Manager call.
	if city, ok := server.geoip.LookupCity(net.ParseIP("81.2.69.142")); !ok || city.CountryCode != "GB" {
		t.Fatalf("direct LookupCity(81.2.69.142) ok=%v city=%+v, want ok=true CountryCode=GB", ok, city)
	}
}

// TestEnrichIPRowsNoOpWhenGeoIPNil covers the cheap-path: a server
// constructed without ever wiring geoip (older test paths predating
// Task 14) must leave rows alone. enrichIPRows has an explicit nil
// guard precisely so the existing handler tests — which never load a
// Manager — do not regress when GeoIP is plumbed in.
func TestEnrichIPRowsNoOpWhenGeoIPNil(t *testing.T) {
	now := time.Date(2026, time.May, 4, 10, 40, 0, 0, time.UTC)
	server := testServerWithSQLite(t, now)
	// Force the nil branch — testServerWithSQLite goes through New(),
	// which assigns a fresh Manager. Setting it to nil here is the
	// only way to exercise the s.geoip == nil short-circuit.
	server.geoip = nil

	rows := []clientIPRow{
		{IPAddress: "81.2.69.142", FirstSeen: now, LastSeen: now},
	}
	server.enrichIPRows(rows)

	if rows[0].CountryCode != "" || rows[0].CountryName != "" || rows[0].City != "" || rows[0].ASN != "" {
		t.Errorf("rows[0] enriched despite nil manager: %+v, want all-empty", rows[0])
	}
}
