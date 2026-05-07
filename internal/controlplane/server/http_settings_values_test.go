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

func testBootstrap(key string) *settingspkg.Bootstrap {
	return &settingspkg.Bootstrap{AuthEncryptionKey: key}
}

func testSourceMap() settingspkg.SourceMap {
	return settingspkg.SourceMap{
		"auth.encryption_key": settingspkg.SourceInfo{Source: settingspkg.SourceEnv, EnvVar: "PANVEX_ENCRYPTION_KEY"},
	}
}
