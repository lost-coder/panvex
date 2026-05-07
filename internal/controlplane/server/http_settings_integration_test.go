package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestSettingsIntegration_FullCycle(t *testing.T) {
	server, _, cookies := newAuthedServer(t)

	// 1. schema returns non-trivial payload
	schema := performJSONRequest(t, server, http.MethodGet, "/api/settings/schema", nil, cookies)
	if !strings.Contains(schema.Body.String(), `"http.listen_address"`) {
		t.Fatal("schema missing http.listen_address")
	}

	// 2. initial values: bootstrap section is locked, operational defaults present
	v0 := performJSONRequest(t, server, http.MethodGet, "/api/settings/values", nil, cookies)
	if v0.Code != http.StatusOK {
		t.Fatalf("initial values: %d %s", v0.Code, v0.Body.String())
	}

	// 3. PUT operational values
	body := map[string]any{
		"auth.password_min_length": 14,
		"http.public_url":          "https://panel.example",
	}
	put := performJSONRequest(t, server, http.MethodPut, "/api/settings/values", body, cookies)
	if put.Code != http.StatusOK {
		t.Fatalf("PUT %d: %s", put.Code, put.Body.String())
	}

	// 4. values reflect change
	v1 := performJSONRequest(t, server, http.MethodGet, "/api/settings/values", nil, cookies)
	var resp struct {
		Operational map[string]struct {
			Value any `json:"value"`
		} `json:"operational"`
	}
	if err := json.Unmarshal(v1.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprintf("%v", resp.Operational["auth.password_min_length"].Value); got != "14" {
		t.Errorf("got %v, want 14", got)
	}
	if got := fmt.Sprintf("%v", resp.Operational["http.public_url"].Value); got != "https://panel.example" {
		t.Errorf("public_url = %v", got)
	}

	// 5. PUT bootstrap is rejected (409)
	bp := performJSONRequest(t, server, http.MethodPut, "/api/settings/values",
		map[string]any{"http.listen_address": ":7777"}, cookies)
	if bp.Code != http.StatusConflict {
		t.Errorf("bootstrap put: status = %d, want 409", bp.Code)
	}

	// 6. restart-status returns no pending fields after operational PUTs
	//    (no field in the registry has restart=true today)
	rs := performJSONRequest(t, server, http.MethodGet, "/api/settings/restart-status", nil, cookies)
	if rs.Code != http.StatusOK {
		t.Fatalf("restart-status: %d %s", rs.Code, rs.Body.String())
	}
	rsBody := rs.Body.String()
	if !strings.Contains(rsBody, `"pending":false`) && !strings.Contains(rsBody, `"pending": false`) {
		t.Errorf("expected pending:false in restart-status, got: %s", rsBody)
	}
}
