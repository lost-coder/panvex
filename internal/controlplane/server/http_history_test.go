package server

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/auth"
	"github.com/lost-coder/panvex/internal/controlplane/history"
	"github.com/lost-coder/panvex/internal/controlplane/storage"
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
		{IPAddress: "10.0.0.1", FirstSeen: now, LastSeen: now},  // private — ShouldLookup skips
		{IPAddress: "not-an-ip", FirstSeen: now, LastSeen: now}, // unparseable — guarded
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

// --- M6/M7 (audit remediation phase 2, task 2.7) ---
//
// M6: parseTimeRangeAt silently ignored unparseable from/to query params
// and never rejected an inverted range (from > to) — both cases used to
// flow straight into storage and come back with an empty/misleading
// result set instead of a 400. M7: when CountUniqueClientIPs errors, the
// handler used to report total_unique = len(ips), which is the
// page-limited count, not the true total — a plausible-looking but wrong
// number.
//
// The three endpoints below all share s.parseTimeRange -> parseTimeRangeAt.
// Admin role resolves to FleetScopeAccess{Global: true} (see
// requireFleetScope), so agentVisibleInScope/clientVisibleInScope pass
// without needing a real agent/client fixture, keeping these tests
// focused on the time-range/count-fallback behaviour.

// countErrStore wraps a real Store and forces CountUniqueClientIPs to
// fail, so the M7 fallback branch in handleClientIPHistory can be
// exercised without needing a storage-layer failure to occur naturally.
type countErrStore struct {
	storage.Store
	err error
}

func (s *countErrStore) CountUniqueClientIPs(_ context.Context, _ string) (int, error) {
	return 0, s.err
}

func historyTestServer(t *testing.T, now time.Time) (*Server, []*http.Cookie) {
	t.Helper()
	server := testServerWithSQLite(t, now)
	cookies := loginAs(t, server, now, "admin", "Admin1password", auth.RoleAdmin)
	return server, cookies
}

// historyEndpoints enumerates the 3 call sites that route through
// parseTimeRangeAt, per the task brief (http_history.go:59/111/194).
func historyEndpoints() []struct {
	name string
	path string
} {
	return []struct {
		name string
		path string
	}{
		{"server-load", "/api/telemetry/servers/srv-1/history/load"},
		{"dc-health", "/api/telemetry/servers/srv-1/history/dc"},
		{"client-ip", "/api/clients/client-1/history/ips"},
	}
}

// TestParseTimeRangeAtRejectsUnparseableParams covers M6: an
// unparseable from/to must be reported as an error, not silently
// dropped in favour of the default window.
func TestParseTimeRangeAtRejectsUnparseableParams(t *testing.T) {
	now := time.Date(2026, time.July, 1, 12, 0, 0, 0, time.UTC)

	cases := []struct {
		name  string
		query string
	}{
		{"garbage from", "from=garbage"},
		{"garbage to", "to=not-a-time"},
		{"garbage from and to", "from=nope&to=nope"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/x?"+tc.query, nil)
			_, _, err := parseTimeRangeAt(r, defaultHistoryRangeHours, now)
			if err == nil {
				t.Fatalf("parseTimeRangeAt(%q) error = nil, want non-nil", tc.query)
			}
		})
	}
}

// TestParseTimeRangeAtRejectsInvertedRange covers M6: from > to must be
// rejected rather than silently flowing to storage (which used to
// return an empty/misleading result set that looked like "no data").
func TestParseTimeRangeAtRejectsInvertedRange(t *testing.T) {
	now := time.Date(2026, time.July, 1, 12, 0, 0, 0, time.UTC)
	later := now.Format(time.RFC3339)
	earlier := now.Add(-time.Hour).Format(time.RFC3339)

	r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/x?from="+later+"&to="+earlier, nil)
	_, _, err := parseTimeRangeAt(r, defaultHistoryRangeHours, now)
	if err == nil {
		t.Fatal("parseTimeRangeAt(from>to) error = nil, want non-nil (inverted range)")
	}
}

// TestParseTimeRangeAtValidRangeStillWorks is the control case: a
// well-formed, non-inverted range must still parse cleanly and the
// existing max-span clamp must still apply.
func TestParseTimeRangeAtValidRangeStillWorks(t *testing.T) {
	now := time.Date(2026, time.July, 1, 12, 0, 0, 0, time.UTC)
	from := now.Add(-2 * time.Hour).Format(time.RFC3339)
	to := now.Format(time.RFC3339)

	r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/x?from="+from+"&to="+to, nil)
	gotFrom, gotTo, err := parseTimeRangeAt(r, defaultHistoryRangeHours, now)
	if err != nil {
		t.Fatalf("parseTimeRangeAt() error = %v, want nil", err)
	}
	if !gotTo.Equal(now) {
		t.Errorf("to = %v, want %v", gotTo, now)
	}
	if wantFrom := now.Add(-2 * time.Hour); !gotFrom.Equal(wantFrom) {
		t.Errorf("from = %v, want %v", gotFrom, wantFrom)
	}

	// Max-span clamp still applies to valid (non-inverted) ranges.
	farFrom := now.Add(-time.Duration(maxHistoryRangeHours+24) * time.Hour).Format(time.RFC3339)
	r2 := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/x?from="+farFrom+"&to="+to, nil)
	clampedFrom, clampedTo, err := parseTimeRangeAt(r2, defaultHistoryRangeHours, now)
	if err != nil {
		t.Fatalf("parseTimeRangeAt(oversized span) error = %v, want nil", err)
	}
	if wantClamped := now.Add(-time.Duration(maxHistoryRangeHours) * time.Hour); !clampedFrom.Equal(wantClamped) {
		t.Errorf("clamped from = %v, want %v", clampedFrom, wantClamped)
	}
	if !clampedTo.Equal(now) {
		t.Errorf("clamped to = %v, want %v", clampedTo, now)
	}
}

// TestHistoryEndpointsReject400OnUnparseableFrom covers M6 end-to-end:
// all 3 handlers that call s.parseTimeRange must answer 400 (not 200
// with an empty/misleading body) when ?from= is garbage.
func TestHistoryEndpointsReject400OnUnparseableFrom(t *testing.T) {
	now := time.Date(2026, time.July, 1, 12, 0, 0, 0, time.UTC)
	server, cookies := historyTestServer(t, now)

	for _, ep := range historyEndpoints() {
		t.Run(ep.name, func(t *testing.T) {
			resp := performJSONRequest(t, server, http.MethodGet, ep.path+"?from=garbage", nil, cookies)
			if resp.Code != http.StatusBadRequest {
				t.Fatalf("GET %s?from=garbage status = %d, want %d; body=%s", ep.path, resp.Code, http.StatusBadRequest, resp.Body.String())
			}
		})
	}
}

// TestHistoryEndpointsReject400OnInvertedRange covers M6 end-to-end:
// all 3 handlers must answer 400 when from > to, instead of silently
// swapping or returning an empty result set.
func TestHistoryEndpointsReject400OnInvertedRange(t *testing.T) {
	now := time.Date(2026, time.July, 1, 12, 0, 0, 0, time.UTC)
	server, cookies := historyTestServer(t, now)

	later := now.Format(time.RFC3339)
	earlier := now.Add(-time.Hour).Format(time.RFC3339)

	for _, ep := range historyEndpoints() {
		t.Run(ep.name, func(t *testing.T) {
			query := "?from=" + later + "&to=" + earlier
			resp := performJSONRequest(t, server, http.MethodGet, ep.path+query, nil, cookies)
			if resp.Code != http.StatusBadRequest {
				t.Fatalf("GET %s%s status = %d, want %d; body=%s", ep.path, query, resp.Code, http.StatusBadRequest, resp.Body.String())
			}
		})
	}
}

// TestHistoryEndpointsAcceptValidRange is the control case for the
// end-to-end 400 tests above: a well-formed range must still succeed.
func TestHistoryEndpointsAcceptValidRange(t *testing.T) {
	now := time.Date(2026, time.July, 1, 12, 0, 0, 0, time.UTC)
	server, cookies := historyTestServer(t, now)

	from := now.Add(-time.Hour).Format(time.RFC3339)
	to := now.Format(time.RFC3339)

	for _, ep := range historyEndpoints() {
		t.Run(ep.name, func(t *testing.T) {
			query := "?from=" + from + "&to=" + to
			resp := performJSONRequest(t, server, http.MethodGet, ep.path+query, nil, cookies)
			if resp.Code != http.StatusOK {
				t.Fatalf("GET %s%s status = %d, want %d; body=%s", ep.path, query, resp.Code, http.StatusOK, resp.Body.String())
			}
		})
	}
}

// TestClientIPHistoryCountErrorDoesNotReportPageSizeAsTotal covers M7:
// when CountUniqueClientIPs fails, the handler must not fall back to
// len(ips) (the page-limited count) as if it were the authoritative
// total_unique. Chosen representation (see task report): total_unique
// stays a plain non-negative integer (0) so the existing strict
// `total_unique: z.number()` frontend schema keeps decoding
// successfully — sending `null` would fail that schema and trip the
// UI's schema-mismatch error boundary, trading a wrong-number bug for
// a broken-panel bug. A sibling total_unique_available=false flag
// carries the "this number is not real" signal for callers that want
// it. The key invariant under test: total_unique must never equal the
// page size (len(ips)) when the count query failed, and
// total_unique_available must be false.
func TestClientIPHistoryCountErrorDoesNotReportPageSizeAsTotal(t *testing.T) {
	now := time.Date(2026, time.July, 1, 12, 0, 0, 0, time.UTC)
	server, cookies := historyTestServer(t, now)

	// Seed a non-empty page of IP rows so len(ips) > 0 — otherwise the
	// old buggy fallback (total_unique = len(ips)) would coincidentally
	// read 0 too, and the test would pass for the wrong reason.
	for _, ip := range []string{"203.0.113.1", "203.0.113.2", "203.0.113.3"} {
		if err := server.store.UpsertClientIPHistory(context.Background(), storage.ClientIPHistoryRecord{
			AgentID:   "agent-1",
			ClientID:  "client-1",
			IPAddress: ip,
			FirstSeen: now.Add(-time.Hour),
			LastSeen:  now,
		}); err != nil {
			t.Fatalf("UpsertClientIPHistory(%s) error = %v", ip, err)
		}
	}

	server.store = &countErrStore{Store: server.store, err: errors.New("count query failed")}
	// The history read facade holds its own store ref, so re-wire it to the
	// count-erroring wrapper the same way the swap above rewired s.store.
	server.historySvc = history.NewService(server.store)

	resp := performJSONRequest(t, server, http.MethodGet, "/api/clients/client-1/history/ips", nil, cookies)
	if resp.Code != http.StatusOK {
		t.Fatalf("GET client ip history status = %d, want %d; body=%s", resp.Code, http.StatusOK, resp.Body.String())
	}

	var payload struct {
		IPs                  []clientIPRow `json:"ips"`
		TotalUnique          int           `json:"total_unique"`
		TotalUniqueAvailable bool          `json:"total_unique_available"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal error = %v; body=%s", err, resp.Body.String())
	}
	if len(payload.IPs) == 0 {
		t.Fatalf("page came back empty; the seeded rows should have been aggregated into ips")
	}
	if payload.TotalUniqueAvailable {
		t.Fatalf("total_unique_available = true, want false (CountUniqueClientIPs errored)")
	}
	if payload.TotalUnique == len(payload.IPs) {
		t.Fatalf("total_unique = %d equals len(ips) = %d; the page-limited count is leaking back out as the total", payload.TotalUnique, len(payload.IPs))
	}
}
