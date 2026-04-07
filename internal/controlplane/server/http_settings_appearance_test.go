package server

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/panvex/panvex/internal/controlplane/auth"
	"github.com/panvex/panvex/internal/controlplane/storage/sqlite"
)

func TestHTTPAppearanceSettingsReadDefaultsAndPersistCurrentUserValues(t *testing.T) {
	now := time.Date(2026, time.March, 21, 14, 0, 0, 0, time.UTC)
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	server := New(Options{
		Now:   func() time.Time { return now },
		Store: store,
	})
	user, _, err := server.auth.BootstrapUser(auth.BootstrapInput{
		Username: "viewer",
		Password: "Viewer1password",
		Role:     auth.RoleViewer,
	}, now)
	if err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}

	loginResponse := performJSONRequest(t, server.Handler(), http.MethodPost, "/api/auth/login", map[string]string{
		"username": "viewer",
		"password": "Viewer1password",
	}, nil)
	if loginResponse.Code != http.StatusOK {
		t.Fatalf("POST /api/auth/login status = %d, want %d", loginResponse.Code, http.StatusOK)
	}
	cookies := loginResponse.Result().Cookies()

	defaultResponse := performJSONRequest(t, server.Handler(), http.MethodGet, "/api/settings/appearance", nil, cookies)
	if defaultResponse.Code != http.StatusOK {
		t.Fatalf("GET /api/settings/appearance status = %d, want %d", defaultResponse.Code, http.StatusOK)
	}

	var defaultPayload struct {
		Theme         string `json:"theme"`
		Density       string `json:"density"`
		HelpMode      string `json:"help_mode"`
		UpdatedAtUnix int64  `json:"updated_at_unix"`
	}
	if err := json.Unmarshal(defaultResponse.Body.Bytes(), &defaultPayload); err != nil {
		t.Fatalf("json.Unmarshal(default) error = %v", err)
	}
	if defaultPayload.Theme != "system" {
		t.Fatalf("default.theme = %q, want %q", defaultPayload.Theme, "system")
	}
	if defaultPayload.Density != "comfortable" {
		t.Fatalf("default.density = %q, want %q", defaultPayload.Density, "comfortable")
	}
	if defaultPayload.HelpMode != "basic" {
		t.Fatalf("default.help_mode = %q, want %q", defaultPayload.HelpMode, "basic")
	}
	if defaultPayload.UpdatedAtUnix != 0 {
		t.Fatalf("default.updated_at_unix = %d, want 0", defaultPayload.UpdatedAtUnix)
	}

	updateResponse := performJSONRequest(t, server.Handler(), http.MethodPut, "/api/settings/appearance", map[string]string{
		"theme":     "dark",
		"density":   "compact",
		"help_mode": "full",
	}, cookies)
	if updateResponse.Code != http.StatusOK {
		t.Fatalf("PUT /api/settings/appearance status = %d, want %d", updateResponse.Code, http.StatusOK)
	}

	var updatedPayload struct {
		Theme         string `json:"theme"`
		Density       string `json:"density"`
		HelpMode      string `json:"help_mode"`
		UpdatedAtUnix int64  `json:"updated_at_unix"`
	}
	if err := json.Unmarshal(updateResponse.Body.Bytes(), &updatedPayload); err != nil {
		t.Fatalf("json.Unmarshal(updated) error = %v", err)
	}
	if updatedPayload.Theme != "dark" {
		t.Fatalf("updated.theme = %q, want %q", updatedPayload.Theme, "dark")
	}
	if updatedPayload.Density != "compact" {
		t.Fatalf("updated.density = %q, want %q", updatedPayload.Density, "compact")
	}
	if updatedPayload.HelpMode != "full" {
		t.Fatalf("updated.help_mode = %q, want %q", updatedPayload.HelpMode, "full")
	}
	if updatedPayload.UpdatedAtUnix != now.Unix() {
		t.Fatalf("updated.updated_at_unix = %d, want %d", updatedPayload.UpdatedAtUnix, now.Unix())
	}

	storedAppearance, err := store.GetUserAppearance(context.Background(), user.ID)
	if err != nil {
		t.Fatalf("store.GetUserAppearance() error = %v", err)
	}
	if storedAppearance.Theme != "dark" {
		t.Fatalf("stored.theme = %q, want %q", storedAppearance.Theme, "dark")
	}
	if storedAppearance.Density != "compact" {
		t.Fatalf("stored.density = %q, want %q", storedAppearance.Density, "compact")
	}
	if storedAppearance.HelpMode != "full" {
		t.Fatalf("stored.help_mode = %q, want %q", storedAppearance.HelpMode, "full")
	}
	if !storedAppearance.UpdatedAt.Equal(now) {
		t.Fatalf("stored.updated_at = %v, want %v", storedAppearance.UpdatedAt, now)
	}
}

func TestHTTPAppearanceSettingsRejectsUnauthorizedAndInvalidValues(t *testing.T) {
	now := time.Date(2026, time.March, 21, 14, 30, 0, 0, time.UTC)
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	server := New(Options{
		Now:   func() time.Time { return now },
		Store: store,
	})
	if _, _, err := server.auth.BootstrapUser(auth.BootstrapInput{
		Username: "viewer",
		Password: "Viewer1password",
		Role:     auth.RoleViewer,
	}, now); err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}

	unauthorizedGet := performJSONRequest(t, server.Handler(), http.MethodGet, "/api/settings/appearance", nil, nil)
	if unauthorizedGet.Code != http.StatusUnauthorized {
		t.Fatalf("GET /api/settings/appearance without session status = %d, want %d", unauthorizedGet.Code, http.StatusUnauthorized)
	}

	unauthorizedPut := performJSONRequest(t, server.Handler(), http.MethodPut, "/api/settings/appearance", map[string]string{
		"theme":     "dark",
		"density":   "compact",
		"help_mode": "full",
	}, nil)
	if unauthorizedPut.Code != http.StatusUnauthorized {
		t.Fatalf("PUT /api/settings/appearance without session status = %d, want %d", unauthorizedPut.Code, http.StatusUnauthorized)
	}

	loginResponse := performJSONRequest(t, server.Handler(), http.MethodPost, "/api/auth/login", map[string]string{
		"username": "viewer",
		"password": "Viewer1password",
	}, nil)
	if loginResponse.Code != http.StatusOK {
		t.Fatalf("POST /api/auth/login status = %d, want %d", loginResponse.Code, http.StatusOK)
	}
	cookies := loginResponse.Result().Cookies()

	invalidTheme := performJSONRequest(t, server.Handler(), http.MethodPut, "/api/settings/appearance", map[string]string{
		"theme":     "sepia",
		"density":   "compact",
		"help_mode": "basic",
	}, cookies)
	if invalidTheme.Code != http.StatusBadRequest {
		t.Fatalf("PUT /api/settings/appearance invalid theme status = %d, want %d", invalidTheme.Code, http.StatusBadRequest)
	}

	invalidDensity := performJSONRequest(t, server.Handler(), http.MethodPut, "/api/settings/appearance", map[string]string{
		"theme":     "dark",
		"density":   "tight",
		"help_mode": "basic",
	}, cookies)
	if invalidDensity.Code != http.StatusBadRequest {
		t.Fatalf("PUT /api/settings/appearance invalid density status = %d, want %d", invalidDensity.Code, http.StatusBadRequest)
	}

	invalidHelpMode := performJSONRequest(t, server.Handler(), http.MethodPut, "/api/settings/appearance", map[string]string{
		"theme":     "dark",
		"density":   "compact",
		"help_mode": "verbose",
	}, cookies)
	if invalidHelpMode.Code != http.StatusBadRequest {
		t.Fatalf("PUT /api/settings/appearance invalid help_mode status = %d, want %d", invalidHelpMode.Code, http.StatusBadRequest)
	}
}

func TestHTTPAppearanceSettingsRequirePersistentStore(t *testing.T) {
	now := time.Date(2026, time.March, 21, 15, 0, 0, 0, time.UTC)
	server := New(Options{
		Now: func() time.Time { return now },
	})
	if _, _, err := server.auth.BootstrapUser(auth.BootstrapInput{
		Username: "viewer",
		Password: "Viewer1password",
		Role:     auth.RoleViewer,
	}, now); err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}

	loginResponse := performJSONRequest(t, server.Handler(), http.MethodPost, "/api/auth/login", map[string]string{
		"username": "viewer",
		"password": "Viewer1password",
	}, nil)
	if loginResponse.Code != http.StatusOK {
		t.Fatalf("POST /api/auth/login status = %d, want %d", loginResponse.Code, http.StatusOK)
	}
	cookies := loginResponse.Result().Cookies()

	getResponse := performJSONRequest(t, server.Handler(), http.MethodGet, "/api/settings/appearance", nil, cookies)
	if getResponse.Code != http.StatusServiceUnavailable {
		t.Fatalf("GET /api/settings/appearance without store status = %d, want %d", getResponse.Code, http.StatusServiceUnavailable)
	}

	putResponse := performJSONRequest(t, server.Handler(), http.MethodPut, "/api/settings/appearance", map[string]string{
		"theme":     "dark",
		"density":   "compact",
		"help_mode": "full",
	}, cookies)
	if putResponse.Code != http.StatusServiceUnavailable {
		t.Fatalf("PUT /api/settings/appearance without store status = %d, want %d", putResponse.Code, http.StatusServiceUnavailable)
	}
}
