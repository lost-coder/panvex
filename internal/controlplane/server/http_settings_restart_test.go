package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	settingspkg2 "github.com/lost-coder/panvex/internal/controlplane/settings"
)

func TestHTTPSettingsRestartStatus_NoneInitially(t *testing.T) {
	server, _, cookies := newAuthedServer(t)
	resp := performJSONRequest(t, server, http.MethodGet, "/api/settings/restart-status", nil, cookies)
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d", resp.Code)
	}
	var body struct {
		Pending bool     `json:"pending"`
		Fields  []string `json:"fields"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Pending {
		t.Errorf("pending should be false on a fresh server")
	}
	if len(body.Fields) != 0 {
		t.Errorf("fields should be empty: %v", body.Fields)
	}
}

func TestHTTPSettingsRestartStatus_FlagsRestartFields(t *testing.T) {
	if !registryHasRestartTrue() {
		t.Skip("no restart=true operational fields declared yet")
	}
	server, _, cookies := newAuthedServer(t)
	updateBody := map[string]any{
		firstRestartTrueField(): exampleValueFor(firstRestartTrueField()),
	}
	put := performJSONRequest(t, server, http.MethodPut, "/api/settings/values", updateBody, cookies)
	if put.Code != http.StatusOK {
		t.Fatalf("PUT failed: %s", put.Body.String())
	}
	resp := performJSONRequest(t, server, http.MethodGet, "/api/settings/restart-status", nil, cookies)
	if !strings.Contains(resp.Body.String(), firstRestartTrueField()) {
		t.Errorf("expected pending field in body:\n%s", resp.Body.String())
	}
}

func registryHasRestartTrue() bool {
	for _, f := range settingspkg2.AllFields() {
		if f.Class == settingspkg2.ClassOperational && f.Restart {
			return true
		}
	}
	return false
}

func firstRestartTrueField() string {
	for _, f := range settingspkg2.AllFields() {
		if f.Class == settingspkg2.ClassOperational && f.Restart {
			return f.Name
		}
	}
	return ""
}

func exampleValueFor(name string) any {
	for _, f := range settingspkg2.AllFields() {
		if f.Name != name {
			continue
		}
		switch f.Type {
		case settingspkg2.TypeInt:
			return 1
		case settingspkg2.TypeBool:
			return true
		case settingspkg2.TypeDuration:
			return "5s"
		default:
			return "x"
		}
	}
	return nil
}
