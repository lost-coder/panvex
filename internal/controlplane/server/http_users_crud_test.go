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

func TestHTTPUsersCreateUpdateDeleteRoundTrip(t *testing.T) {
	now := time.Date(2026, time.March, 16, 21, 0, 0, 0, time.UTC)
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
	cookies := loginResponse.Result().Cookies()

	createResponse := performJSONRequest(t, server.Handler(), http.MethodPost, "/api/users", map[string]string{
		"username": "operator",
		"role":     "operator",
		"password": "operator-password",
	}, cookies)
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("POST /api/users status = %d, want %d", createResponse.Code, http.StatusCreated)
	}

	var createdUser struct {
		ID          string `json:"id"`
		Username    string `json:"username"`
		Role        string `json:"role"`
		TotpEnabled bool   `json:"totp_enabled"`
	}
	if err := json.Unmarshal(createResponse.Body.Bytes(), &createdUser); err != nil {
		t.Fatalf("json.Unmarshal(create) error = %v", err)
	}
	if createdUser.ID == "" {
		t.Fatal("created user id = empty, want persisted user")
	}
	if createdUser.Username != "operator" {
		t.Fatalf("created username = %q, want %q", createdUser.Username, "operator")
	}
	if createdUser.Role != "operator" {
		t.Fatalf("created role = %q, want %q", createdUser.Role, "operator")
	}
	if createdUser.TotpEnabled {
		t.Fatal("created totp_enabled = true, want false")
	}

	updateResponse := performJSONRequest(t, server.Handler(), http.MethodPut, "/api/users/"+createdUser.ID, map[string]string{
		"username":     "viewer-renamed",
		"role":         "viewer",
		"new_password": "viewer-password",
	}, cookies)
	if updateResponse.Code != http.StatusOK {
		t.Fatalf("PUT /api/users/{id} status = %d, want %d", updateResponse.Code, http.StatusOK)
	}

	var updatedUser struct {
		ID       string `json:"id"`
		Username string `json:"username"`
		Role     string `json:"role"`
	}
	if err := json.Unmarshal(updateResponse.Body.Bytes(), &updatedUser); err != nil {
		t.Fatalf("json.Unmarshal(update) error = %v", err)
	}
	if updatedUser.Username != "viewer-renamed" {
		t.Fatalf("updated username = %q, want %q", updatedUser.Username, "viewer-renamed")
	}
	if updatedUser.Role != "viewer" {
		t.Fatalf("updated role = %q, want %q", updatedUser.Role, "viewer")
	}

	userLogin := performJSONRequest(t, server.Handler(), http.MethodPost, "/api/auth/login", map[string]string{
		"username": "viewer-renamed",
		"password": "viewer-password",
	}, nil)
	if userLogin.Code != http.StatusOK {
		t.Fatalf("POST /api/auth/login updated user status = %d, want %d", userLogin.Code, http.StatusOK)
	}

	deleteResponse := performJSONRequest(t, server.Handler(), http.MethodDelete, "/api/users/"+createdUser.ID, nil, cookies)
	if deleteResponse.Code != http.StatusNoContent {
		t.Fatalf("DELETE /api/users/{id} status = %d, want %d", deleteResponse.Code, http.StatusNoContent)
	}

	usersResponse := performJSONRequest(t, server.Handler(), http.MethodGet, "/api/users", nil, cookies)
	if usersResponse.Code != http.StatusOK {
		t.Fatalf("GET /api/users status = %d, want %d", usersResponse.Code, http.StatusOK)
	}

	var usersPayload []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(usersResponse.Body.Bytes(), &usersPayload); err != nil {
		t.Fatalf("json.Unmarshal(list users) error = %v", err)
	}
	if len(usersPayload) != 1 {
		t.Fatalf("len(users) = %d, want %d", len(usersPayload), 1)
	}
}

func TestHTTPUsersRejectSelfDeleteAndLastAdminDemotion(t *testing.T) {
	now := time.Date(2026, time.March, 16, 21, 20, 0, 0, time.UTC)
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	server := New(Options{
		Now:   func() time.Time { return now },
		Store: store,
	})
	adminUser, _, err := server.auth.BootstrapUser(auth.BootstrapInput{
		Username: "admin",
		Password: "admin-password",
		Role:     auth.RoleAdmin,
	}, now)
	if err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}

	loginResponse := performJSONRequest(t, server.Handler(), http.MethodPost, "/api/auth/login", map[string]string{
		"username": "admin",
		"password": "admin-password",
	}, nil)
	if loginResponse.Code != http.StatusOK {
		t.Fatalf("POST /api/auth/login status = %d, want %d", loginResponse.Code, http.StatusOK)
	}
	cookies := loginResponse.Result().Cookies()

	deleteResponse := performJSONRequest(t, server.Handler(), http.MethodDelete, "/api/users/"+adminUser.ID, nil, cookies)
	if deleteResponse.Code != http.StatusBadRequest {
		t.Fatalf("DELETE /api/users/self status = %d, want %d", deleteResponse.Code, http.StatusBadRequest)
	}

	demoteResponse := performJSONRequest(t, server.Handler(), http.MethodPut, "/api/users/"+adminUser.ID, map[string]string{
		"username": adminUser.Username,
		"role":     "viewer",
	}, cookies)
	if demoteResponse.Code != http.StatusBadRequest {
		t.Fatalf("PUT /api/users/{id} last admin demotion status = %d, want %d", demoteResponse.Code, http.StatusBadRequest)
	}
}
