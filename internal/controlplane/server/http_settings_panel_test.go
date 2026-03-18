package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/panvex/panvex/internal/controlplane/auth"
	"github.com/panvex/panvex/internal/controlplane/storage/sqlite"
)

func TestHTTPPanelSettingsRequiresAdminAndPersistsChanges(t *testing.T) {
	now := time.Date(2026, time.March, 16, 19, 0, 0, 0, time.UTC)
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
			GRPCListenAddress: ":8443",
			TLSMode:           "proxy",
			RestartSupported:  true,
		},
	})
	if _, _, err := server.auth.BootstrapUser(auth.BootstrapInput{
		Username: "admin",
		Password: "admin-password",
		Role:     auth.RoleAdmin,
	}, now); err != nil {
		t.Fatalf("BootstrapUser(admin) error = %v", err)
	}
	if _, _, err := server.auth.BootstrapUser(auth.BootstrapInput{
		Username: "viewer",
		Password: "viewer-password",
		Role:     auth.RoleViewer,
	}, now); err != nil {
		t.Fatalf("BootstrapUser(viewer) error = %v", err)
	}

	viewerLogin := performJSONRequest(t, server.Handler(), http.MethodPost, "/api/auth/login", map[string]string{
		"username": "viewer",
		"password": "viewer-password",
	}, nil)
	if viewerLogin.Code != http.StatusOK {
		t.Fatalf("POST /api/auth/login viewer status = %d, want %d", viewerLogin.Code, http.StatusOK)
	}

	viewerSettings := performJSONRequest(t, server.Handler(), http.MethodGet, "/api/settings/panel", nil, viewerLogin.Result().Cookies())
	if viewerSettings.Code != http.StatusForbidden {
		t.Fatalf("GET /api/settings/panel as viewer status = %d, want %d", viewerSettings.Code, http.StatusForbidden)
	}

	adminLogin := performJSONRequest(t, server.Handler(), http.MethodPost, "/api/auth/login", map[string]string{
		"username": "admin",
		"password": "admin-password",
	}, nil)
	if adminLogin.Code != http.StatusOK {
		t.Fatalf("POST /api/auth/login admin status = %d, want %d", adminLogin.Code, http.StatusOK)
	}
	adminCookies := adminLogin.Result().Cookies()

	initialResponse := performJSONRequest(t, server.Handler(), http.MethodGet, "/api/settings/panel", nil, adminCookies)
	if initialResponse.Code != http.StatusOK {
		t.Fatalf("GET /api/settings/panel status = %d, want %d", initialResponse.Code, http.StatusOK)
	}

	var initialPayload struct {
		HTTPListenAddress string `json:"http_listen_address"`
		GRPCListenAddress string `json:"grpc_listen_address"`
		TLSMode           string `json:"tls_mode"`
		Restart           struct {
			Supported bool   `json:"supported"`
			Pending   bool   `json:"pending"`
			State     string `json:"state"`
		} `json:"restart"`
	}
	if err := json.Unmarshal(initialResponse.Body.Bytes(), &initialPayload); err != nil {
		t.Fatalf("json.Unmarshal(initial) error = %v", err)
	}
	if initialPayload.HTTPListenAddress != ":8080" {
		t.Fatalf("initial.http_listen_address = %q, want %q", initialPayload.HTTPListenAddress, ":8080")
	}
	if initialPayload.GRPCListenAddress != ":8443" {
		t.Fatalf("initial.grpc_listen_address = %q, want %q", initialPayload.GRPCListenAddress, ":8443")
	}
	if initialPayload.TLSMode != "proxy" {
		t.Fatalf("initial.tls_mode = %q, want %q", initialPayload.TLSMode, "proxy")
	}
	if !initialPayload.Restart.Supported {
		t.Fatal("initial.restart.supported = false, want true")
	}
	if initialPayload.Restart.Pending {
		t.Fatal("initial.restart.pending = true, want false")
	}
	if initialPayload.Restart.State != "ready" {
		t.Fatalf("initial.restart.state = %q, want %q", initialPayload.Restart.State, "ready")
	}

	updateResponse := performJSONRequest(t, server.Handler(), http.MethodPut, "/api/settings/panel", map[string]string{
		"http_public_url":      "https://panel.example.com",
		"http_root_path":       "/panvex",
		"grpc_public_endpoint": "grpc.panel.example.com:443",
		"http_listen_address":  ":8080",
		"grpc_listen_address":  ":8443",
		"tls_mode":             "proxy",
		"tls_cert_file":        "",
		"tls_key_file":         "",
	}, adminCookies)
	if updateResponse.Code != http.StatusOK {
		t.Fatalf("PUT /api/settings/panel status = %d, want %d", updateResponse.Code, http.StatusOK)
	}

	var updatedPayload struct {
		HTTPPublicURL      string `json:"http_public_url"`
		HTTPRootPath       string `json:"http_root_path"`
		GRPCPublicEndpoint string `json:"grpc_public_endpoint"`
		Restart            struct {
			Supported bool   `json:"supported"`
			Pending   bool   `json:"pending"`
			State     string `json:"state"`
		} `json:"restart"`
	}
	if err := json.Unmarshal(updateResponse.Body.Bytes(), &updatedPayload); err != nil {
		t.Fatalf("json.Unmarshal(updated) error = %v", err)
	}
	if updatedPayload.HTTPPublicURL != "https://panel.example.com" {
		t.Fatalf("updated.http_public_url = %q, want %q", updatedPayload.HTTPPublicURL, "https://panel.example.com")
	}
	if updatedPayload.HTTPRootPath != "/panvex" {
		t.Fatalf("updated.http_root_path = %q, want %q", updatedPayload.HTTPRootPath, "/panvex")
	}
	if updatedPayload.GRPCPublicEndpoint != "grpc.panel.example.com:443" {
		t.Fatalf("updated.grpc_public_endpoint = %q, want %q", updatedPayload.GRPCPublicEndpoint, "grpc.panel.example.com:443")
	}
	if !updatedPayload.Restart.Supported {
		t.Fatal("updated.restart.supported = false, want true")
	}
	if !updatedPayload.Restart.Pending {
		t.Fatal("updated.restart.pending = false, want true")
	}
	if updatedPayload.Restart.State != "pending" {
		t.Fatalf("updated.restart.state = %q, want %q", updatedPayload.Restart.State, "pending")
	}

	storedSettings, err := store.GetPanelSettings(context.Background())
	if err != nil {
		t.Fatalf("GetPanelSettings() error = %v", err)
	}
	if storedSettings.HTTPPublicURL != "https://panel.example.com" {
		t.Fatalf("stored.http_public_url = %q, want %q", storedSettings.HTTPPublicURL, "https://panel.example.com")
	}
	if storedSettings.HTTPRootPath != "/panvex" {
		t.Fatalf("stored.http_root_path = %q, want %q", storedSettings.HTTPRootPath, "/panvex")
	}
	if storedSettings.GRPCPublicEndpoint != "grpc.panel.example.com:443" {
		t.Fatalf("stored.grpc_public_endpoint = %q, want %q", storedSettings.GRPCPublicEndpoint, "grpc.panel.example.com:443")
	}
}

func TestHTTPPanelSettingsMarksRestartUnavailableWhenRuntimeCannotSelfRestart(t *testing.T) {
	now := time.Date(2026, time.March, 16, 19, 30, 0, 0, time.UTC)
	server := New(Options{
		Now: func() time.Time { return now },
		PanelRuntime: PanelRuntime{
			HTTPListenAddress: ":8080",
			GRPCListenAddress: ":8443",
			TLSMode:           "proxy",
			RestartSupported:  false,
		},
	})
	if _, _, err := server.auth.BootstrapUser(auth.BootstrapInput{
		Username: "admin",
		Password: "admin-password",
		Role:     auth.RoleAdmin,
	}, now); err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}

	loginResponse := performJSONRequest(t, server.Handler(), http.MethodPost, "/api/auth/login", map[string]string{
		"username": "admin",
		"password": "admin-password",
	}, nil)
	if loginResponse.Code != http.StatusOK {
		t.Fatalf("POST /api/auth/login status = %d, want %d", loginResponse.Code, http.StatusOK)
	}

	settingsResponse := performJSONRequest(t, server.Handler(), http.MethodGet, "/api/settings/panel", nil, loginResponse.Result().Cookies())
	if settingsResponse.Code != http.StatusOK {
		t.Fatalf("GET /api/settings/panel status = %d, want %d", settingsResponse.Code, http.StatusOK)
	}

	var payload struct {
		Restart struct {
			Supported bool   `json:"supported"`
			Pending   bool   `json:"pending"`
			State     string `json:"state"`
		} `json:"restart"`
	}
	if err := json.Unmarshal(settingsResponse.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if payload.Restart.Supported {
		t.Fatal("restart.supported = true, want false")
	}
	if payload.Restart.Pending {
		t.Fatal("restart.pending = true, want false")
	}
	if payload.Restart.State != "unavailable" {
		t.Fatalf("restart.state = %q, want %q", payload.Restart.State, "unavailable")
	}
}

func TestHTTPPanelSettingsRejectsDirectTLSWithoutCertificateFiles(t *testing.T) {
	now := time.Date(2026, time.March, 17, 10, 40, 0, 0, time.UTC)
	server := New(Options{
		Now: func() time.Time { return now },
		PanelRuntime: PanelRuntime{
			HTTPListenAddress: ":8080",
			GRPCListenAddress: ":8443",
			TLSMode:           "proxy",
			RestartSupported:  true,
		},
	})
	if _, _, err := server.auth.BootstrapUser(auth.BootstrapInput{
		Username: "admin",
		Password: "admin-password",
		Role:     auth.RoleAdmin,
	}, now); err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}

	loginResponse := performJSONRequest(t, server.Handler(), http.MethodPost, "/api/auth/login", map[string]string{
		"username": "admin",
		"password": "admin-password",
	}, nil)
	if loginResponse.Code != http.StatusOK {
		t.Fatalf("POST /api/auth/login status = %d, want %d", loginResponse.Code, http.StatusOK)
	}

	updateResponse := performJSONRequest(t, server.Handler(), http.MethodPut, "/api/settings/panel", map[string]string{
		"http_public_url":      "https://panel.example.com",
		"http_root_path":       "/panvex",
		"grpc_public_endpoint": "grpc.panel.example.com:443",
		"http_listen_address":  ":8080",
		"grpc_listen_address":  ":8443",
		"tls_mode":             "direct",
		"tls_cert_file":        "",
		"tls_key_file":         "",
	}, loginResponse.Result().Cookies())
	if updateResponse.Code != http.StatusBadRequest {
		t.Fatalf("PUT /api/settings/panel status = %d, want %d", updateResponse.Code, http.StatusBadRequest)
	}
}

func TestHTTPPanelRestartRequiresAdminAndInvokesRuntimeHook(t *testing.T) {
	now := time.Date(2026, time.March, 17, 1, 20, 0, 0, time.UTC)
	restartRequests := make(chan struct{}, 1)
	server := New(Options{
		Now: func() time.Time { return now },
		PanelRuntime: PanelRuntime{
			HTTPListenAddress: ":8080",
			GRPCListenAddress: ":8443",
			TLSMode:           "proxy",
			RestartSupported:  true,
		},
		RequestRestart: func() error {
			restartRequests <- struct{}{}
			return nil
		},
	})
	if _, _, err := server.auth.BootstrapUser(auth.BootstrapInput{
		Username: "admin",
		Password: "admin-password",
		Role:     auth.RoleAdmin,
	}, now); err != nil {
		t.Fatalf("BootstrapUser(admin) error = %v", err)
	}
	if _, _, err := server.auth.BootstrapUser(auth.BootstrapInput{
		Username: "viewer",
		Password: "viewer-password",
		Role:     auth.RoleViewer,
	}, now); err != nil {
		t.Fatalf("BootstrapUser(viewer) error = %v", err)
	}

	viewerLogin := performJSONRequest(t, server.Handler(), http.MethodPost, "/api/auth/login", map[string]string{
		"username": "viewer",
		"password": "viewer-password",
	}, nil)
	if viewerLogin.Code != http.StatusOK {
		t.Fatalf("POST /api/auth/login viewer status = %d, want %d", viewerLogin.Code, http.StatusOK)
	}

	viewerRestart := performJSONRequest(t, server.Handler(), http.MethodPost, "/api/settings/panel/restart", nil, viewerLogin.Result().Cookies())
	if viewerRestart.Code != http.StatusForbidden {
		t.Fatalf("POST /api/settings/panel/restart as viewer status = %d, want %d", viewerRestart.Code, http.StatusForbidden)
	}

	adminLogin := performJSONRequest(t, server.Handler(), http.MethodPost, "/api/auth/login", map[string]string{
		"username": "admin",
		"password": "admin-password",
	}, nil)
	if adminLogin.Code != http.StatusOK {
		t.Fatalf("POST /api/auth/login admin status = %d, want %d", adminLogin.Code, http.StatusOK)
	}

	adminRestart := performJSONRequest(t, server.Handler(), http.MethodPost, "/api/settings/panel/restart", nil, adminLogin.Result().Cookies())
	if adminRestart.Code != http.StatusAccepted {
		t.Fatalf("POST /api/settings/panel/restart status = %d, want %d", adminRestart.Code, http.StatusAccepted)
	}

	select {
	case <-restartRequests:
	case <-time.After(time.Second):
		t.Fatal("request restart hook was not called")
	}
}

func TestHTTPPanelRestartRejectsUnsupportedRuntime(t *testing.T) {
	now := time.Date(2026, time.March, 17, 1, 30, 0, 0, time.UTC)
	server := New(Options{
		Now: func() time.Time { return now },
		PanelRuntime: PanelRuntime{
			HTTPListenAddress: ":8080",
			GRPCListenAddress: ":8443",
			TLSMode:           "proxy",
			RestartSupported:  false,
		},
	})
	if _, _, err := server.auth.BootstrapUser(auth.BootstrapInput{
		Username: "admin",
		Password: "admin-password",
		Role:     auth.RoleAdmin,
	}, now); err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}

	loginResponse := performJSONRequest(t, server.Handler(), http.MethodPost, "/api/auth/login", map[string]string{
		"username": "admin",
		"password": "admin-password",
	}, nil)
	if loginResponse.Code != http.StatusOK {
		t.Fatalf("POST /api/auth/login status = %d, want %d", loginResponse.Code, http.StatusOK)
	}

	restartResponse := performJSONRequest(t, server.Handler(), http.MethodPost, "/api/settings/panel/restart", nil, loginResponse.Result().Cookies())
	if restartResponse.Code != http.StatusConflict {
		t.Fatalf("POST /api/settings/panel/restart status = %d, want %d", restartResponse.Code, http.StatusConflict)
	}
}

func TestHTTPPanelRestartReturnsInternalErrorWhenRuntimeHookFails(t *testing.T) {
	now := time.Date(2026, time.March, 18, 13, 0, 0, 0, time.UTC)
	server := New(Options{
		Now: func() time.Time { return now },
		PanelRuntime: PanelRuntime{
			HTTPListenAddress: ":8080",
			GRPCListenAddress: ":8443",
			TLSMode:           "proxy",
			RestartSupported:  true,
		},
		RequestRestart: func() error {
			return errors.New("restart hook failed")
		},
	})
	if _, _, err := server.auth.BootstrapUser(auth.BootstrapInput{
		Username: "admin",
		Password: "admin-password",
		Role:     auth.RoleAdmin,
	}, now); err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}

	loginResponse := performJSONRequest(t, server.Handler(), http.MethodPost, "/api/auth/login", map[string]string{
		"username": "admin",
		"password": "admin-password",
	}, nil)
	if loginResponse.Code != http.StatusOK {
		t.Fatalf("POST /api/auth/login status = %d, want %d", loginResponse.Code, http.StatusOK)
	}

	restartResponse := performJSONRequest(t, server.Handler(), http.MethodPost, "/api/settings/panel/restart", nil, loginResponse.Result().Cookies())
	if restartResponse.Code != http.StatusInternalServerError {
		t.Fatalf("POST /api/settings/panel/restart status = %d, want %d", restartResponse.Code, http.StatusInternalServerError)
	}
}
