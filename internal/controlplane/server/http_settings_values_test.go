package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/auth"
	settingspkg "github.com/lost-coder/panvex/internal/controlplane/settings"
)

func TestOperationalEntrySurfacesSource(t *testing.T) {
	srv := testServerWithSQLite(t, time.Date(2026, time.May, 29, 10, 0, 0, 0, time.UTC))
	ctx := context.Background()
	if err := srv.settings.Put(ctx, map[string]string{"http.public_url": "https://p.example"}, "test"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	var set, unset settingspkg.FieldMeta
	for _, f := range settingspkg.AllFields() {
		if f.Name == "http.public_url" {
			set = f
		}
		if f.Name == "grpc.public_endpoint" {
			unset = f // operational, default "", never set here
		}
	}
	if got := srv.operationalEntry(set).Source; got != "db" {
		t.Errorf("set field Source = %q, want db", got)
	}
	if got := srv.operationalEntry(unset).Source; got != "default" {
		t.Errorf("unset field Source = %q, want default (was hardcoded db)", got)
	}
	if srv.operationalEntry(set).OverriddenByEnv {
		t.Errorf("http.public_url should not be env-overridden")
	}
}

func TestBootstrapEntryReportsApplyTier(t *testing.T) {
	srv := testServerWithSQLite(t, time.Date(2026, time.May, 29, 10, 0, 0, 0, time.UTC))
	var dsn settingspkg.FieldMeta
	for _, f := range settingspkg.AllFields() {
		if f.Name == "storage.dsn" {
			dsn = f
		}
	}
	if got := srv.bootstrapEntry(dsn).Apply; got != "config" {
		t.Errorf("storage.dsn Apply = %q, want config", got)
	}
}

func TestHTTPSettingsValues_ReturnsOperationalAndBootstrap(t *testing.T) {
	server, _, cookies := newAuthedServer(t)

	resp := performJSONRequest(t, server, http.MethodGet, "/api/settings/values", nil, cookies)
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d", resp.Code)
	}
	var body struct {
		Bootstrap   map[string]map[string]any `json:"bootstrap"`
		Operational map[string]map[string]any `json:"operational"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	v, ok := body.Operational["auth.password_min_length"]
	if !ok {
		t.Fatal("missing auth.password_min_length")
	}
	if v["locked"] != false {
		t.Errorf("operational locked = %v", v["locked"])
	}
	// Never set on this fixture, so its truthful source is the registry
	// default (was previously hardcoded "db" before the source surfacing fix).
	if v["source"] != "default" {
		t.Errorf("operational source = %v", v["source"])
	}
}

func TestHTTPSettingsValues_RedactsSecrets(t *testing.T) {
	server, _, cookies := newAuthedServer(t)
	server.SetTestBootstrap(testBootstrap("k3y"), testSourceMap())

	resp := performJSONRequest(t, server, http.MethodGet, "/api/settings/values", nil, cookies)
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d", resp.Code)
	}
	body := resp.Body.String()
	if strings.Contains(body, "k3y") {
		t.Fatal("secret value leaked into /values response")
	}
	if !strings.Contains(body, `"value":"***"`) && !strings.Contains(body, `"value": "***"`) {
		t.Errorf("expected redacted *** marker; body:\n%s", body)
	}
}

func TestHTTPSettingsValues_PutOperationalSucceeds(t *testing.T) {
	server, _, cookies := newAuthedServer(t)
	body := map[string]any{
		"auth.password_min_length": 18,
		"updates.channel":          "beta",
	}
	resp := performJSONRequest(t, server, http.MethodPut, "/api/settings/values", body, cookies)
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", resp.Code, resp.Body.String())
	}
	got := performJSONRequest(t, server, http.MethodGet, "/api/settings/values", nil, cookies)
	if !strings.Contains(got.Body.String(), `"value":18`) && !strings.Contains(got.Body.String(), `"value": 18`) {
		t.Errorf("password_min_length not updated:\n%s", got.Body.String())
	}
}

func TestHTTPSettingsValues_PutBootstrapRejected(t *testing.T) {
	server, _, cookies := newAuthedServer(t)
	body := map[string]any{
		"tls.mode": "direct",
	}
	resp := performJSONRequest(t, server, http.MethodPut, "/api/settings/values", body, cookies)
	if resp.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body = %s", resp.Code, resp.Body.String())
	}
}

func TestHTTPSettingsValues_PutInvalidValueRejected(t *testing.T) {
	server, _, cookies := newAuthedServer(t)
	body := map[string]any{
		"auth.password_min_length": 3,
	}
	resp := performJSONRequest(t, server, http.MethodPut, "/api/settings/values", body, cookies)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.Code)
	}
}

// TestHTTPSettingsValues_PutPasswordPolicyAppliesLive asserts that a PUT
// of auth.password_min_length is mirrored into the auth service's live
// policy (S-01) — the store-backed write path must not leave the enforced
// minimum stale.
func TestHTTPSettingsValues_PutPasswordPolicyAppliesLive(t *testing.T) {
	server, _, cookies := newAuthedServer(t)

	if got := server.auth.EffectivePasswordMinLength(); got != 10 {
		t.Fatalf("baseline policy = %d, want compiled default 10", got)
	}

	body := map[string]any{"auth.password_min_length": 20}
	resp := performJSONRequest(t, server, http.MethodPut, "/api/settings/values", body, cookies)
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", resp.Code, resp.Body.String())
	}

	if got := server.auth.EffectivePasswordMinLength(); got != 20 {
		t.Errorf("policy after PUT = %d, want 20 (not applied live)", got)
	}
}

// TestHTTPSettingsValues_RedactsDSN asserts the postgres DSN (which embeds
// the password) is masked to *** in GET /settings/values once set, now that
// storage.dsn carries secret=true.
func TestHTTPSettingsValues_RedactsDSN(t *testing.T) {
	server, _, cookies := newAuthedServer(t)
	const secretDSN = "postgres://user:sup3rsecret@db:5432/panvex"
	server.SetTestBootstrap(&settingspkg.Bootstrap{StorageDSN: secretDSN}, settingspkg.SourceMap{})

	resp := performJSONRequest(t, server, http.MethodGet, "/api/settings/values", nil, cookies)
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d", resp.Code)
	}
	body := resp.Body.String()
	if strings.Contains(body, "sup3rsecret") || strings.Contains(body, secretDSN) {
		t.Fatalf("DSN secret leaked into /values response:\n%s", body)
	}
	var parsed struct {
		Bootstrap map[string]map[string]any `json:"bootstrap"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &parsed); err != nil {
		t.Fatal(err)
	}
	dsn, ok := parsed.Bootstrap["storage.dsn"]
	if !ok {
		t.Fatal("missing storage.dsn in bootstrap")
	}
	if dsn["value"] != "***" {
		t.Errorf("storage.dsn value = %v, want *** (masked)", dsn["value"])
	}
	if dsn["secret"] != true {
		t.Errorf("storage.dsn secret = %v, want true", dsn["secret"])
	}
}

// TestHTTPSettingsValues_PutRequiresStore asserts that the values PUT path
// returns 503 (not a nil-deref panic) on a no-store server where s.settings is
// nil. The route is registered unconditionally; without the guard the
// s.settings.Put/PasswordMinLength calls would panic (B1).
func TestHTTPSettingsValues_PutRequiresStore(t *testing.T) {
	now := time.Date(2026, time.March, 21, 15, 0, 0, 0, time.UTC)
	server := mustNew(t, Options{
		LoginTimingFloor: -1,
		Now:              func() time.Time { return now },
	})
	if _, _, err := server.auth.BootstrapUser(context.Background(), auth.BootstrapInput{
		Username: "admin",
		Password: "Admin1password",
		Role:     auth.RoleAdmin,
	}, now); err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}

	loginResp := performJSONRequest(t, server, http.MethodPost, "/api/auth/login", map[string]string{
		"username": "admin",
		"password": "Admin1password",
	}, nil)
	if loginResp.Code != http.StatusOK {
		t.Fatalf("login status = %d", loginResp.Code)
	}
	cookies := loginResp.Result().Cookies()

	resp := performJSONRequest(t, server, http.MethodPut, "/api/settings/values", map[string]any{
		"auth.password_min_length": 18,
	}, cookies)
	if resp.Code != http.StatusServiceUnavailable {
		t.Fatalf("PUT /api/settings/values without store status = %d, want %d", resp.Code, http.StatusServiceUnavailable)
	}
}

func testBootstrap(key string) *settingspkg.Bootstrap {
	return &settingspkg.Bootstrap{AuthEncryptionKey: key}
}

func testSourceMap() settingspkg.SourceMap {
	return settingspkg.SourceMap{
		"auth.encryption_key": settingspkg.SourceInfo{Source: settingspkg.SourceEnv, EnvVar: "PANVEX_ENCRYPTION_KEY"},
	}
}
