package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/auth"
	"github.com/lost-coder/panvex/internal/controlplane/geoip"
)

// loginAs bootstraps the named user with the requested role, performs a
// JSON login, and returns the resulting cookies. Mirrors the pattern
// used by http_settings_appearance_test.go and http_retention_test.go;
// extracted into one helper because every geoip case needs the same
// admin handshake.
func loginAs(t *testing.T, server *Server, now time.Time, username, password string, role auth.Role) []*http.Cookie {
	t.Helper()
	if _, _, err := server.auth.BootstrapUser(context.Background(), auth.BootstrapInput{
		Username: username,
		Password: password,
		Role:     role,
	}, now); err != nil {
		t.Fatalf("BootstrapUser(%s) error = %v", username, err)
	}
	resp := performJSONRequest(t, server, http.MethodPost, "/api/auth/login", map[string]string{
		"username": username,
		"password": password,
	}, nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("POST /api/auth/login(%s) status = %d, want %d; body=%s", username, resp.Code, http.StatusOK, resp.Body.String())
	}
	return resp.Result().Cookies()
}

// copyGeoIPFixture copies the package-bundled MaxMind test database to
// dst. The mmdb files under internal/controlplane/geoip/testdata/ are
// already used by lookup/manager tests; reusing them keeps the geoip
// surface fully self-contained (no network, no operator-provided
// fixtures).
func copyGeoIPFixture(t *testing.T, src, dst string) {
	t.Helper()
	in, err := os.Open(src)
	if err != nil {
		t.Fatalf("open fixture %s: %v", src, err)
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		t.Fatalf("create %s: %v", dst, err)
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		t.Fatalf("copy fixture: %v", err)
	}
}

// TestGeoIPSettingsGetReturnsDefaultsOnFreshServer covers Task 15 #1 —
// a freshly initialised server has mode="" (Disabled), zero Source and
// zero State. Confirms restoreGeoIPSettings's "missing blob is fine"
// branch and that handleGetGeoIPSettings emits the canonical response
// envelope (settings + state) the panel UI relies on.
func TestGeoIPSettingsGetReturnsDefaultsOnFreshServer(t *testing.T) {
	now := time.Date(2026, time.May, 4, 10, 0, 0, 0, time.UTC)
	t.Setenv("PANVEX_GEOIP_DIR", t.TempDir())

	server := testServerWithSQLite(t, now)
	cookies := loginAs(t, server, now, "admin", "Admin1password", auth.RoleAdmin)

	resp := performJSONRequest(t, server, http.MethodGet, "/api/settings/geoip", nil, cookies)
	if resp.Code != http.StatusOK {
		t.Fatalf("GET /api/settings/geoip status = %d, want %d; body=%s", resp.Code, http.StatusOK, resp.Body.String())
	}
	var payload geoipResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal error = %v", err)
	}
	if payload.Settings.Mode != geoip.ModeDisabled {
		t.Fatalf("Settings.Mode = %q, want %q", payload.Settings.Mode, geoip.ModeDisabled)
	}
	if payload.Settings.City != (geoip.Source{}) {
		t.Fatalf("Settings.City = %+v, want zero", payload.Settings.City)
	}
	if payload.Settings.ASN != (geoip.Source{}) {
		t.Fatalf("Settings.ASN = %+v, want zero", payload.Settings.ASN)
	}
	if payload.State != (geoip.State{}) {
		t.Fatalf("State = %+v, want zero", payload.State)
	}
}

// TestGeoIPSettingsPutRejectsUnknownMode covers Task 15 #2 — the
// validation switch in validateGeoIPSettings only accepts the four
// canonical Mode values. An arbitrary string must be rejected as 400
// before any persistence side-effect runs.
func TestGeoIPSettingsPutRejectsUnknownMode(t *testing.T) {
	now := time.Date(2026, time.May, 4, 10, 5, 0, 0, time.UTC)
	t.Setenv("PANVEX_GEOIP_DIR", t.TempDir())

	server := testServerWithSQLite(t, now)
	cookies := loginAs(t, server, now, "admin", "Admin1password", auth.RoleAdmin)

	resp := performJSONRequest(t, server, http.MethodPut, "/api/settings/geoip", map[string]any{
		"mode": "weird",
	}, cookies)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("PUT /api/settings/geoip status = %d, want %d; body=%s", resp.Code, http.StatusBadRequest, resp.Body.String())
	}
}

// TestGeoIPSettingsPutRequiresAtLeastOneSourceEnabled covers Task 15 #3
// — switching out of Disabled mode without enabling either the City or
// ASN source is meaningless and must fail fast with 400.
func TestGeoIPSettingsPutRequiresAtLeastOneSourceEnabled(t *testing.T) {
	now := time.Date(2026, time.May, 4, 10, 10, 0, 0, time.UTC)
	t.Setenv("PANVEX_GEOIP_DIR", t.TempDir())

	server := testServerWithSQLite(t, now)
	cookies := loginAs(t, server, now, "admin", "Admin1password", auth.RoleAdmin)

	resp := performJSONRequest(t, server, http.MethodPut, "/api/settings/geoip", geoip.Settings{
		Mode: geoip.ModeAuto,
		City: geoip.Source{Enabled: false},
		ASN:  geoip.Source{Enabled: false},
	}, cookies)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("PUT /api/settings/geoip status = %d, want %d; body=%s", resp.Code, http.StatusBadRequest, resp.Body.String())
	}
}

// TestGeoIPSettingsPutAcceptsLocalModeWithReadablePaths covers Task 15
// #4 — the happy path for ModeLocal: both .mmdb files are absolute and
// readable, validateGeoIPSettings accepts them, the handler persists
// the settings and reloads the manager. The returned response echoes
// settings.mode == "local" and the State remains zero (the put path
// does not trigger a refresh; state is only populated by the worker
// or an explicit POST /refresh).
func TestGeoIPSettingsPutAcceptsLocalModeWithReadablePaths(t *testing.T) {
	now := time.Date(2026, time.May, 4, 10, 15, 0, 0, time.UTC)
	tmp := t.TempDir()
	t.Setenv("PANVEX_GEOIP_DIR", tmp)

	cityPath := filepath.Join(tmp, "city.mmdb")
	asnPath := filepath.Join(tmp, "asn.mmdb")
	copyGeoIPFixture(t, "../geoip/testdata/GeoLite2-City-Test.mmdb", cityPath)
	copyGeoIPFixture(t, "../geoip/testdata/GeoLite2-ASN-Test.mmdb", asnPath)

	server := testServerWithSQLite(t, now)
	cookies := loginAs(t, server, now, "admin", "Admin1password", auth.RoleAdmin)

	body := geoip.Settings{
		Mode: geoip.ModeLocal,
		City: geoip.Source{Enabled: true, LocalPath: cityPath},
		ASN:  geoip.Source{Enabled: true, LocalPath: asnPath},
	}
	resp := performJSONRequest(t, server, http.MethodPut, "/api/settings/geoip", body, cookies)
	if resp.Code != http.StatusOK {
		t.Fatalf("PUT /api/settings/geoip status = %d, want %d; body=%s", resp.Code, http.StatusOK, resp.Body.String())
	}
	var payload geoipResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal error = %v", err)
	}
	if payload.Settings.Mode != geoip.ModeLocal {
		t.Fatalf("Settings.Mode = %q, want %q", payload.Settings.Mode, geoip.ModeLocal)
	}
	if payload.Settings.City.LocalPath != cityPath {
		t.Fatalf("Settings.City.LocalPath = %q, want %q", payload.Settings.City.LocalPath, cityPath)
	}
	if payload.Settings.ASN.LocalPath != asnPath {
		t.Fatalf("Settings.ASN.LocalPath = %q, want %q", payload.Settings.ASN.LocalPath, asnPath)
	}
	// Confirm the round-trip: a follow-up GET sees the same persisted
	// values, proving handlePutGeoIPSettings actually committed and
	// the in-memory snapshot agrees with what the handler returned.
	getResp := performJSONRequest(t, server, http.MethodGet, "/api/settings/geoip", nil, cookies)
	if getResp.Code != http.StatusOK {
		t.Fatalf("GET /api/settings/geoip after PUT status = %d, want %d", getResp.Code, http.StatusOK)
	}
	var getPayload geoipResponse
	if err := json.Unmarshal(getResp.Body.Bytes(), &getPayload); err != nil {
		t.Fatalf("json.Unmarshal(get) error = %v", err)
	}
	if getPayload.Settings.Mode != geoip.ModeLocal {
		t.Fatalf("GET Settings.Mode = %q, want %q", getPayload.Settings.Mode, geoip.ModeLocal)
	}
}

// TestGeoIPSettingsPutRejectsLocalModeWithRelativePath covers the
// "filepath.IsAbs" branch of validateGeoIPSettings. Operators
// occasionally paste relative paths from a CI runner — the handler
// must reject them before they hit Manager.Reload.
func TestGeoIPSettingsPutRejectsLocalModeWithRelativePath(t *testing.T) {
	now := time.Date(2026, time.May, 4, 10, 20, 0, 0, time.UTC)
	t.Setenv("PANVEX_GEOIP_DIR", t.TempDir())

	server := testServerWithSQLite(t, now)
	cookies := loginAs(t, server, now, "admin", "Admin1password", auth.RoleAdmin)

	resp := performJSONRequest(t, server, http.MethodPut, "/api/settings/geoip", geoip.Settings{
		Mode: geoip.ModeLocal,
		City: geoip.Source{Enabled: true, LocalPath: "relative/city.mmdb"},
	}, cookies)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("PUT /api/settings/geoip status = %d, want %d; body=%s", resp.Code, http.StatusBadRequest, resp.Body.String())
	}
}

// TestGeoIPSettingsPutRejectsLocalModePathOracle covers the path-injection
// hardening (CodeQL go/path-injection #9/#10): a local_path that is absolute
// but points at a non-.mmdb file or contains traversal must be rejected, so an
// admin cannot turn the existence/size stat into an arbitrary-file oracle.
func TestGeoIPSettingsPutRejectsLocalModePathOracle(t *testing.T) {
	now := time.Date(2026, time.May, 4, 10, 22, 0, 0, time.UTC)
	t.Setenv("PANVEX_GEOIP_DIR", t.TempDir())
	server := testServerWithSQLite(t, now)
	cookies := loginAs(t, server, now, "admin", "Admin1password", auth.RoleAdmin)

	for _, bad := range []string{"/etc/passwd", "/etc/shadow", "/var/lib/geoip/../../etc/passwd.mmdb"} {
		resp := performJSONRequest(t, server, http.MethodPut, "/api/settings/geoip", geoip.Settings{
			Mode: geoip.ModeLocal,
			City: geoip.Source{Enabled: true, LocalPath: bad},
		}, cookies)
		if resp.Code != http.StatusBadRequest {
			t.Fatalf("LocalPath %q: status = %d, want %d (path-oracle must be rejected)", bad, resp.Code, http.StatusBadRequest)
		}
	}
}

// TestRunGeoIPUpdateRejectsInternalURL covers the GeoIP egress guard: a
// URL-mode source pointing at an internal/link-local address (the cloud
// metadata endpoint here) must be refused at dial time, never reaching the
// network, even though the host is no longer on a GitHub allow-list.
func TestRunGeoIPUpdateRejectsInternalURL(t *testing.T) {
	now := time.Date(2026, time.May, 4, 10, 24, 0, 0, time.UTC)
	t.Setenv("PANVEX_GEOIP_DIR", t.TempDir())
	server := testServerWithSQLite(t, now)

	server.settingsMu.Lock()
	server.geoipSettings = geoip.Settings{
		Mode: geoip.ModeURL,
		City: geoip.Source{Enabled: true, URL: "https://169.254.169.254/GeoLite2-City.mmdb"},
	}
	server.settingsMu.Unlock()

	state := server.runGeoIPUpdate(t.Context(), geoip.KindCity)
	if state.Error == "" {
		t.Fatal("runGeoIPUpdate accepted an internal URL; SSRF egress guard missing")
	}
	if !strings.Contains(state.Error, "non-public address") {
		t.Fatalf("state.Error = %q, want it to mention the blocked non-public address", state.Error)
	}
}

// TestGeoIPSettingsRejectsNonAdmin covers Task 15 #5 — the routes are
// registered under the requireMinimumRole(RoleAdmin) router, so a
// signed-in viewer must receive 403. An unauthenticated request must
// receive 401. Both cases protect the panel from accidental exposure
// of the settings surface to operators / read-only accounts.
func TestGeoIPSettingsRejectsNonAdmin(t *testing.T) {
	now := time.Date(2026, time.May, 4, 10, 25, 0, 0, time.UTC)
	t.Setenv("PANVEX_GEOIP_DIR", t.TempDir())

	server := testServerWithSQLite(t, now)

	// Unauthenticated: no cookies → 401 from requireSession (or its
	// upstream auth middleware). Status must NOT be 200; we assert
	// "not OK" rather than a strict code because the auth pipeline
	// may pick either 401 or 403 depending on cookie presence.
	noCookieResp := performJSONRequest(t, server, http.MethodGet, "/api/settings/geoip", nil, nil)
	if noCookieResp.Code != http.StatusUnauthorized && noCookieResp.Code != http.StatusForbidden {
		t.Fatalf("unauth GET /api/settings/geoip status = %d, want 401 or 403; body=%s", noCookieResp.Code, noCookieResp.Body.String())
	}

	// Authenticated viewer: the requireMinimumRole(Admin) middleware
	// returns 403. This is the more important assertion — it proves
	// the route is actually behind the admin gate.
	cookies := loginAs(t, server, now, "viewer", "Viewer1password", auth.RoleViewer)
	resp := performJSONRequest(t, server, http.MethodGet, "/api/settings/geoip", nil, cookies)
	if resp.Code != http.StatusForbidden {
		t.Fatalf("viewer GET /api/settings/geoip status = %d, want %d; body=%s", resp.Code, http.StatusForbidden, resp.Body.String())
	}

	putResp := performJSONRequest(t, server, http.MethodPut, "/api/settings/geoip", geoip.Settings{Mode: geoip.ModeDisabled}, cookies)
	if putResp.Code != http.StatusForbidden {
		t.Fatalf("viewer PUT /api/settings/geoip status = %d, want %d; body=%s", putResp.Code, http.StatusForbidden, putResp.Body.String())
	}

	refreshResp := performJSONRequest(t, server, http.MethodPost, "/api/settings/geoip/refresh", nil, cookies)
	if refreshResp.Code != http.StatusForbidden {
		t.Fatalf("viewer POST /api/settings/geoip/refresh status = %d, want %d; body=%s", refreshResp.Code, http.StatusForbidden, refreshResp.Body.String())
	}
}

// TestGeoIPRefreshDisabledModeReturnsZeroState covers Task 15 #6 — the
// refresh handler must remain a safe no-op when GeoIP is disabled.
// runGeoIPUpdate short-circuits on !src.Enabled and Mode==Disabled, so
// the response surface is the current (zero) state, never an error.
// This is the contract the panel UI relies on to show "never refreshed"
// after the operator toggled the feature off.
func TestGeoIPRefreshDisabledModeReturnsZeroState(t *testing.T) {
	now := time.Date(2026, time.May, 4, 10, 30, 0, 0, time.UTC)
	t.Setenv("PANVEX_GEOIP_DIR", t.TempDir())

	server := testServerWithSQLite(t, now)
	cookies := loginAs(t, server, now, "admin", "Admin1password", auth.RoleAdmin)

	resp := performJSONRequest(t, server, http.MethodPost, "/api/settings/geoip/refresh", nil, cookies)
	if resp.Code != http.StatusOK {
		t.Fatalf("POST /api/settings/geoip/refresh status = %d, want %d; body=%s", resp.Code, http.StatusOK, resp.Body.String())
	}
	var payload geoipResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal error = %v", err)
	}
	if payload.Settings.Mode != geoip.ModeDisabled {
		t.Fatalf("Settings.Mode = %q, want %q", payload.Settings.Mode, geoip.ModeDisabled)
	}
	// runGeoIPUpdate sets LastCheckedAt before short-circuiting on
	// !src.Enabled / Mode==Disabled, so the state row is "we checked,
	// nothing to do" rather than fully zero. Every other field must
	// remain unset — no Path, no ETag, no Error, no SizeBytes — and
	// LastUpdatedAt stays 0 because no file was actually written.
	for _, kv := range []struct {
		name  string
		state geoip.SourceState
	}{
		{"city", payload.State.City},
		{"asn", payload.State.ASN},
	} {
		if kv.state.LastUpdatedAt != 0 {
			t.Fatalf("%s.LastUpdatedAt = %d, want 0", kv.name, kv.state.LastUpdatedAt)
		}
		if kv.state.ETag != "" || kv.state.Path != "" || kv.state.Error != "" || kv.state.SizeBytes != 0 {
			t.Fatalf("%s state non-zero beyond LastCheckedAt: %+v", kv.name, kv.state)
		}
	}
}
