package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	settingspkg "github.com/lost-coder/panvex/internal/controlplane/settings"
)

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
	if v["source"] != "db" {
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
		"http.listen_address": ":7777",
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

func testBootstrap(key string) *settingspkg.Bootstrap {
	return &settingspkg.Bootstrap{AuthEncryptionKey: key}
}

func testSourceMap() settingspkg.SourceMap {
	return settingspkg.SourceMap{
		"auth.encryption_key": settingspkg.SourceInfo{Source: settingspkg.SourceEnv, EnvVar: "PANVEX_ENCRYPTION_KEY"},
	}
}
