package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
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
	//    (apply=restart operational fields exist, e.g. auth.session_*)
	rs := performJSONRequest(t, server, http.MethodGet, "/api/settings/restart-status", nil, cookies)
	if rs.Code != http.StatusOK {
		t.Fatalf("restart-status: %d %s", rs.Code, rs.Body.String())
	}
	rsBody := rs.Body.String()
	if !strings.Contains(rsBody, `"pending":false`) && !strings.Contains(rsBody, `"pending": false`) {
		t.Errorf("expected pending:false in restart-status, got: %s", rsBody)
	}
}

func TestSettingsIntegration_AuditedFieldsRoundTrip(t *testing.T) {
	server, _, cookies := newAuthedServer(t)

	body := map[string]any{
		"auth.password_lockout_max_attempts":  7,
		"observability.metrics_poll_interval": "10s",
	}
	put := performJSONRequest(t, server, http.MethodPut, "/api/settings/values", body, cookies)
	if put.Code != http.StatusOK {
		t.Fatalf("PUT %d: %s", put.Code, put.Body.String())
	}

	store := server.Settings()
	if got := store.AuthPasswordLockoutMaxAttempts(); got != 7 {
		t.Errorf("getter still %d, want 7", got)
	}
	if got := store.MetricsPollInterval(); got != 10*time.Second {
		t.Errorf("getter still %v, want 10s", got)
	}
}
