package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/panvex/panvex/internal/controlplane/auth"
	"github.com/panvex/panvex/internal/controlplane/storage/sqlite"
)

func TestServerLoginSetsSessionAndReturnsMe(t *testing.T) {
	now := time.Date(2026, time.March, 14, 8, 0, 0, 0, time.UTC)
	server := New(Options{
		Now: func() time.Time { return now },
	})
	if _, _, err := server.auth.BootstrapUser(auth.BootstrapInput{
		Username: "viewer",
		Password: "viewer-password",
		Role:     auth.RoleViewer,
	}, now); err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}

	loginResponse := performJSONRequest(t, server.Handler(), http.MethodPost, "/auth/login", map[string]string{
		"username": "viewer",
		"password": "viewer-password",
	}, nil)
	if loginResponse.Code != http.StatusOK {
		t.Fatalf("POST /auth/login status = %d, want %d", loginResponse.Code, http.StatusOK)
	}

	cookies := loginResponse.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("POST /auth/login returned no cookies")
	}

	meResponse := performJSONRequest(t, server.Handler(), http.MethodGet, "/auth/me", nil, cookies)
	if meResponse.Code != http.StatusOK {
		t.Fatalf("GET /auth/me status = %d, want %d", meResponse.Code, http.StatusOK)
	}

	var payload struct {
		Username string `json:"username"`
		Role     string `json:"role"`
	}
	if err := json.Unmarshal(meResponse.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if payload.Username != "viewer" {
		t.Fatalf("payload.Username = %q, want %q", payload.Username, "viewer")
	}

	if payload.Role != string(auth.RoleViewer) {
		t.Fatalf("payload.Role = %q, want %q", payload.Role, auth.RoleViewer)
	}
}

func TestServerCreateJobRejectsViewerRole(t *testing.T) {
	now := time.Date(2026, time.March, 14, 8, 0, 0, 0, time.UTC)
	server := New(Options{
		Now: func() time.Time { return now },
	})
	if _, _, err := server.auth.BootstrapUser(auth.BootstrapInput{
		Username: "viewer",
		Password: "viewer-password",
		Role:     auth.RoleViewer,
	}, now); err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}
	server.agents["agent-1"] = Agent{
		ID:           "agent-1",
		NodeName:     "node-a",
		EnvironmentID:"prod",
		FleetGroupID: "ams-1",
		ReadOnly:     false,
	}

	loginResponse := performJSONRequest(t, server.Handler(), http.MethodPost, "/auth/login", map[string]string{
		"username": "viewer",
		"password": "viewer-password",
	}, nil)

	jobResponse := performJSONRequest(t, server.Handler(), http.MethodPost, "/jobs", map[string]any{
		"action":           "runtime.reload",
		"target_agent_ids": []string{"agent-1"},
		"idempotency_key":  "job-1",
		"ttl_seconds":      60,
	}, loginResponse.Result().Cookies())
	if jobResponse.Code != http.StatusForbidden {
		t.Fatalf("POST /jobs status = %d, want %d", jobResponse.Code, http.StatusForbidden)
	}
}

func TestServerCreateJobAcceptsOperatorWithTotp(t *testing.T) {
	now := time.Date(2026, time.March, 14, 8, 0, 0, 0, time.UTC)
	server := New(Options{
		Now: func() time.Time { return now },
	})
	_, secret, err := server.auth.BootstrapUser(auth.BootstrapInput{
		Username: "operator",
		Password: "operator-password",
		Role:     auth.RoleOperator,
	}, now)
	if err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}
	server.agents["agent-1"] = Agent{
		ID:           "agent-1",
		NodeName:     "node-a",
		EnvironmentID:"prod",
		FleetGroupID: "ams-1",
		ReadOnly:     false,
	}

	code, err := server.auth.GenerateTotpCode(secret, now)
	if err != nil {
		t.Fatalf("GenerateTotpCode() error = %v", err)
	}

	loginResponse := performJSONRequest(t, server.Handler(), http.MethodPost, "/auth/login", map[string]string{
		"username":  "operator",
		"password":  "operator-password",
		"totp_code": code,
	}, nil)
	if loginResponse.Code != http.StatusOK {
		t.Fatalf("POST /auth/login status = %d, want %d", loginResponse.Code, http.StatusOK)
	}

	jobResponse := performJSONRequest(t, server.Handler(), http.MethodPost, "/jobs", map[string]any{
		"action":           "runtime.reload",
		"target_agent_ids": []string{"agent-1"},
		"idempotency_key":  "job-1",
		"ttl_seconds":      60,
	}, loginResponse.Result().Cookies())
	if jobResponse.Code != http.StatusAccepted {
		t.Fatalf("POST /jobs status = %d, want %d", jobResponse.Code, http.StatusAccepted)
	}
}

func TestServerNewDoesNotReseedExistingStoreUsers(t *testing.T) {
	now := time.Date(2026, time.March, 15, 9, 0, 0, 0, time.UTC)
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	seeded := auth.NewServiceWithStore(store)
	user, _, err := seeded.BootstrapUser(auth.BootstrapInput{
		Username: "admin",
		Password: "current-password",
		Role:     auth.RoleViewer,
	}, now)
	if err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}

	server := New(Options{
		Now: func() time.Time { return now.Add(time.Minute) },
		Users: []auth.User{
			{
				ID:           user.ID,
				Username:     user.Username,
				PasswordHash: "stale-hash",
				Role:         user.Role,
				CreatedAt:    user.CreatedAt,
			},
		},
		Store: store,
	})

	if _, err := server.auth.Authenticate(auth.LoginInput{
		Username: "admin",
		Password: "current-password",
	}, now.Add(2*time.Minute)); err != nil {
		t.Fatalf("Authenticate() with stored password error = %v", err)
	}

	if _, err := server.auth.Authenticate(auth.LoginInput{
		Username: "admin",
		Password: "stale-password",
	}, now.Add(2*time.Minute)); err != auth.ErrInvalidCredentials {
		t.Fatalf("Authenticate() with stale password error = %v, want %v", err, auth.ErrInvalidCredentials)
	}
}

func performJSONRequest(t *testing.T, handler http.Handler, method string, path string, body any, cookies []*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()

	var payload []byte
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("json.Marshal() error = %v", err)
		}
		payload = encoded
	}

	request := httptest.NewRequest(method, path, bytes.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}
