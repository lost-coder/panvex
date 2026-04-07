package server

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/panvex/panvex/internal/controlplane/auth"
	"github.com/panvex/panvex/internal/controlplane/storage/sqlite"
)

func TestHTTPEnrollmentTokensExposeConfiguredPanelURL(t *testing.T) {
	now := time.Date(2026, time.March, 17, 10, 50, 0, 0, time.UTC)
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	server := New(Options{
		Now:   func() time.Time { return now },
		Store: store,
		PanelRuntime: PanelRuntime{
			HTTPListenAddress: ":8080",
			HTTPRootPath:      "/panvex",
			GRPCListenAddress: ":8443",
			TLSMode:           "proxy",
		},
	})
	if _, _, err := server.auth.BootstrapUser(auth.BootstrapInput{
		Username: "operator",
		Password: "Operator1password",
		Role:     auth.RoleOperator,
	}, now); err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}

	loginResponse := performJSONRequest(
		t,
		server.Handler(),
		http.MethodPost,
		"/panvex/api/auth/login",
		map[string]string{
			"username": "operator",
			"password": "Operator1password",
		},
		nil,
	)
	if loginResponse.Code != http.StatusOK {
		t.Fatalf("POST /api/auth/login status = %d, want %d", loginResponse.Code, http.StatusOK)
	}
	cookies := loginResponse.Result().Cookies()

	settingsResponse := performJSONRequest(
		t,
		server.Handler(),
		http.MethodPut,
		"/panvex/api/settings/panel",
		map[string]string{
			"http_public_url":      "https://panel.example.com",
			"grpc_public_endpoint": "grpc.panel.example.com:443",
		},
		cookies,
	)
	if settingsResponse.Code != http.StatusForbidden {
		t.Fatalf("PUT /api/settings/panel as operator status = %d, want %d", settingsResponse.Code, http.StatusForbidden)
	}

	if _, _, err := server.auth.BootstrapUser(auth.BootstrapInput{
		Username: "admin",
		Password: "Admin1password",
		Role:     auth.RoleAdmin,
	}, now); err != nil {
		t.Fatalf("BootstrapUser(admin) error = %v", err)
	}

	adminLogin := performJSONRequest(
		t,
		server.Handler(),
		http.MethodPost,
		"/panvex/api/auth/login",
		map[string]string{
			"username": "admin",
			"password": "Admin1password",
		},
		nil,
	)
	if adminLogin.Code != http.StatusOK {
		t.Fatalf("POST /api/auth/login admin status = %d, want %d", adminLogin.Code, http.StatusOK)
	}
	adminCookies := adminLogin.Result().Cookies()

	updateResponse := performJSONRequest(
		t,
		server.Handler(),
		http.MethodPut,
		"/panvex/api/settings/panel",
		map[string]string{
			"http_public_url":      "https://panel.example.com",
			"grpc_public_endpoint": "grpc.panel.example.com:443",
		},
		adminCookies,
	)
	if updateResponse.Code != http.StatusOK {
		t.Fatalf("PUT /api/settings/panel admin status = %d, want %d", updateResponse.Code, http.StatusOK)
	}

	createResponse := performJSONRequestWithHeaders(
		t,
		server.Handler(),
		http.MethodPost,
		"https://internal.example.net/panvex/api/agents/enrollment-tokens",
		map[string]any{
			"fleet_group_id": "default",
			"ttl_seconds":    600,
		},
		cookies,
		nil,
	)
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("POST /api/agents/enrollment-tokens status = %d, want %d", createResponse.Code, http.StatusCreated)
	}

	var createdToken struct {
		Value    string `json:"value"`
		PanelURL string `json:"panel_url"`
	}
	if err := json.Unmarshal(createResponse.Body.Bytes(), &createdToken); err != nil {
		t.Fatalf("json.Unmarshal(create token) error = %v", err)
	}
	if createdToken.PanelURL != "https://panel.example.com/panvex" {
		t.Fatalf("create.panel_url = %q, want %q", createdToken.PanelURL, "https://panel.example.com/panvex")
	}

	listResponse := performJSONRequestWithHeaders(
		t,
		server.Handler(),
		http.MethodGet,
		"https://internal.example.net/panvex/api/agents/enrollment-tokens",
		nil,
		cookies,
		nil,
	)
	if listResponse.Code != http.StatusOK {
		t.Fatalf("GET /api/agents/enrollment-tokens status = %d, want %d", listResponse.Code, http.StatusOK)
	}

	var listedTokens []struct {
		Value    string `json:"value"`
		PanelURL string `json:"panel_url"`
	}
	if err := json.Unmarshal(listResponse.Body.Bytes(), &listedTokens); err != nil {
		t.Fatalf("json.Unmarshal(list tokens) error = %v", err)
	}
	if len(listedTokens) != 1 {
		t.Fatalf("len(tokens) = %d, want %d", len(listedTokens), 1)
	}
	if listedTokens[0].PanelURL != "https://panel.example.com/panvex" {
		t.Fatalf("list.panel_url = %q, want %q", listedTokens[0].PanelURL, "https://panel.example.com/panvex")
	}
}
