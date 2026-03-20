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

func TestHTTPPanelSettingsRequiresAdminAndPersistsSharedEndpointChanges(t *testing.T) {
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
		HTTPRootPath       string `json:"http_root_path"`
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
	if initialPayload.HTTPRootPath != "" {
		t.Fatalf("initial.http_root_path = %q, want empty", initialPayload.HTTPRootPath)
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
		"grpc_public_endpoint": "grpc.panel.example.com:443",
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
	if updatedPayload.HTTPRootPath != "" {
		t.Fatalf("updated.http_root_path = %q, want empty", updatedPayload.HTTPRootPath)
	}
	if updatedPayload.GRPCPublicEndpoint != "grpc.panel.example.com:443" {
		t.Fatalf("updated.grpc_public_endpoint = %q, want %q", updatedPayload.GRPCPublicEndpoint, "grpc.panel.example.com:443")
	}
	if !updatedPayload.Restart.Supported {
		t.Fatal("updated.restart.supported = false, want true")
	}
	if updatedPayload.Restart.Pending {
		t.Fatal("updated.restart.pending = true, want false")
	}
	if updatedPayload.Restart.State != "ready" {
		t.Fatalf("updated.restart.state = %q, want %q", updatedPayload.Restart.State, "ready")
	}

	storedSettings, err := store.GetPanelSettings(context.Background())
	if err != nil {
		t.Fatalf("GetPanelSettings() error = %v", err)
	}
	if storedSettings.HTTPPublicURL != "https://panel.example.com" {
		t.Fatalf("stored.http_public_url = %q, want %q", storedSettings.HTTPPublicURL, "https://panel.example.com")
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

func TestHTTPPanelSettingsRejectsRuntimeMutationsInLegacyMode(t *testing.T) {
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
	}, loginResponse.Result().Cookies())
	if updateResponse.Code != http.StatusBadRequest {
		t.Fatalf("PUT /api/settings/panel status = %d, want %d", updateResponse.Code, http.StatusBadRequest)
	}
}

func TestHTTPPanelSettingsExposesConfigManagedRuntimeAsReadOnly(t *testing.T) {
	now := time.Date(2026, time.March, 20, 20, 0, 0, 0, time.UTC)
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	server := New(Options{
		Now:   func() time.Time { return now },
		Store: store,
		PanelRuntime: PanelRuntime{
			HTTPListenAddress: ":18080",
			HTTPRootPath:      "/runtime",
			GRPCListenAddress: ":18443",
			TLSMode:           "direct",
			TLSCertFile:       "/etc/panvex/tls/panel.crt",
			TLSKeyFile:        "/etc/panvex/tls/panel.key",
			RestartSupported:  true,
			ConfigSource:      PanelRuntimeSourceConfigFile,
			ConfigPath:        "/etc/panvex/config.toml",
		},
	})
	if _, _, err := server.auth.BootstrapUser(auth.BootstrapInput{
		Username: "admin",
		Password: "admin-password",
		Role:     auth.RoleAdmin,
	}, now); err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}

	loginResponse := performJSONRequest(t, server.Handler(), http.MethodPost, "/runtime/api/auth/login", map[string]string{
		"username": "admin",
		"password": "admin-password",
	}, nil)
	if loginResponse.Code != http.StatusOK {
		t.Fatalf("POST /api/auth/login status = %d, want %d", loginResponse.Code, http.StatusOK)
	}
	cookies := loginResponse.Result().Cookies()

	updateResponse := performJSONRequest(t, server.Handler(), http.MethodPut, "/runtime/api/settings/panel", map[string]string{
		"http_public_url":      "https://panel.example.com",
		"grpc_public_endpoint": "grpc.panel.example.com:443",
	}, cookies)
	if updateResponse.Code != http.StatusOK {
		t.Fatalf("PUT /api/settings/panel status = %d, want %d", updateResponse.Code, http.StatusOK)
	}

	var payload struct {
		HTTPRootPath       string `json:"http_root_path"`
		HTTPListenAddress  string `json:"http_listen_address"`
		GRPCListenAddress  string `json:"grpc_listen_address"`
		TLSMode            string `json:"tls_mode"`
		TLSCertFile        string `json:"tls_cert_file"`
		TLSKeyFile         string `json:"tls_key_file"`
		RuntimeSource      string `json:"runtime_source"`
		RuntimeConfigPath  string `json:"runtime_config_path"`
		Restart            struct {
			Pending bool   `json:"pending"`
			State   string `json:"state"`
		} `json:"restart"`
	}
	if err := json.Unmarshal(updateResponse.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if payload.HTTPRootPath != "/runtime" {
		t.Fatalf("payload.http_root_path = %q, want %q", payload.HTTPRootPath, "/runtime")
	}
	if payload.HTTPListenAddress != ":18080" {
		t.Fatalf("payload.http_listen_address = %q, want %q", payload.HTTPListenAddress, ":18080")
	}
	if payload.GRPCListenAddress != ":18443" {
		t.Fatalf("payload.grpc_listen_address = %q, want %q", payload.GRPCListenAddress, ":18443")
	}
	if payload.TLSMode != "direct" {
		t.Fatalf("payload.tls_mode = %q, want %q", payload.TLSMode, "direct")
	}
	if payload.TLSCertFile != "/etc/panvex/tls/panel.crt" {
		t.Fatalf("payload.tls_cert_file = %q, want %q", payload.TLSCertFile, "/etc/panvex/tls/panel.crt")
	}
	if payload.TLSKeyFile != "/etc/panvex/tls/panel.key" {
		t.Fatalf("payload.tls_key_file = %q, want %q", payload.TLSKeyFile, "/etc/panvex/tls/panel.key")
	}
	if payload.RuntimeSource != PanelRuntimeSourceConfigFile {
		t.Fatalf("payload.runtime_source = %q, want %q", payload.RuntimeSource, PanelRuntimeSourceConfigFile)
	}
	if payload.RuntimeConfigPath != "/etc/panvex/config.toml" {
		t.Fatalf("payload.runtime_config_path = %q, want %q", payload.RuntimeConfigPath, "/etc/panvex/config.toml")
	}
	if payload.Restart.Pending {
		t.Fatal("payload.restart.pending = true, want false")
	}
	if payload.Restart.State != "ready" {
		t.Fatalf("payload.restart.state = %q, want %q", payload.Restart.State, "ready")
	}
}

func TestHTTPPanelSettingsRejectsRuntimeMutationsWhenConfigManagesRuntime(t *testing.T) {
	now := time.Date(2026, time.March, 20, 20, 30, 0, 0, time.UTC)
	server := New(Options{
		Now: func() time.Time { return now },
		PanelRuntime: PanelRuntime{
			HTTPListenAddress: ":18080",
			HTTPRootPath:      "/runtime",
			GRPCListenAddress: ":18443",
			TLSMode:           "proxy",
			RestartSupported:  true,
			ConfigSource:      PanelRuntimeSourceConfigFile,
			ConfigPath:        "/etc/panvex/config.toml",
		},
	})
	if _, _, err := server.auth.BootstrapUser(auth.BootstrapInput{
		Username: "admin",
		Password: "admin-password",
		Role:     auth.RoleAdmin,
	}, now); err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}

	loginResponse := performJSONRequest(t, server.Handler(), http.MethodPost, "/runtime/api/auth/login", map[string]string{
		"username": "admin",
		"password": "admin-password",
	}, nil)
	if loginResponse.Code != http.StatusOK {
		t.Fatalf("POST /api/auth/login status = %d, want %d", loginResponse.Code, http.StatusOK)
	}

	updateResponse := performJSONRequest(t, server.Handler(), http.MethodPut, "/runtime/api/settings/panel", map[string]string{
		"http_public_url":      "https://panel.example.com",
		"http_root_path":       "/mutated",
		"grpc_public_endpoint": "grpc.panel.example.com:443",
		"http_listen_address":  ":9999",
		"grpc_listen_address":  ":9998",
		"tls_mode":             "direct",
		"tls_cert_file":        "/etc/panvex/other.crt",
		"tls_key_file":         "/etc/panvex/other.key",
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
