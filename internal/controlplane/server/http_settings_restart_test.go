package server

import (
	"encoding/json"
	"fmt"
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
		// Use the field's Max bound when present — it always passes range
		// validation and is guaranteed to differ from the default so the
		// active-snapshot comparison detects a change.
		if f.Max != "" {
			switch f.Type {
			case settingspkg2.TypeInt:
				var n int
				if _, err := fmt.Sscanf(f.Max, "%d", &n); err == nil {
					return n
				}
			case settingspkg2.TypeBool:
				return f.Max == "true"
			default:
				return f.Max
			}
		}
		// No Max — use the default as a safe fallback (may not trigger a
		// change detection, but at least the PUT will not fail validation).
		if f.HasDefault && f.Default != "" {
			return f.Default
		}
		// Last resort: generic type-appropriate value.
		switch f.Type {
		case settingspkg2.TypeInt:
			return 1
		case settingspkg2.TypeBool:
			return true
		case settingspkg2.TypeDuration:
			return "1h"
		default:
			return "x"
		}
	}
	return nil
}
