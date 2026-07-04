package server

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/auth"
	"github.com/lost-coder/panvex/internal/controlplane/storage/sqlite"
)

func TestHTTPUsersCreateUpdateDeleteRoundTrip(t *testing.T) {
	now := time.Date(2026, time.March, 16, 21, 0, 0, 0, time.UTC)
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	server := mustNew(t, Options{
		LoginTimingFloor: -1,
		Now:              func() time.Time { return now },
		Store:            store,
	})
	defer server.Close()
	if _, _, err := server.auth.BootstrapUser(context.Background(), auth.BootstrapInput{
		Username: "admin",
		Password: "Admin1password",
		Role:     auth.RoleAdmin,
	}, now); err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}

	loginResponse := performJSONRequest(t, server, http.MethodPost, "/api/auth/login", map[string]string{
		"username": "admin",
		"password": "Admin1password",
	}, nil)
	if loginResponse.Code != http.StatusOK {
		t.Fatalf("POST /api/auth/login status = %d, want %d", loginResponse.Code, http.StatusOK)
	}
	cookies := loginResponse.Result().Cookies()

	createResponse := performJSONRequest(t, server, http.MethodPost, "/api/users", map[string]string{
		"username": "operator",
		"role":     "operator",
		"password": "Operator1password",
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

	updateResponse := performJSONRequest(t, server, http.MethodPut, "/api/users/"+createdUser.ID, map[string]string{
		"username":     "viewer-renamed",
		"role":         "viewer",
		"new_password": "Viewer1password",
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

	userLogin := performJSONRequest(t, server, http.MethodPost, "/api/auth/login", map[string]string{
		"username": "viewer-renamed",
		"password": "Viewer1password",
	}, nil)
	if userLogin.Code != http.StatusOK {
		t.Fatalf("POST /api/auth/login updated user status = %d, want %d", userLogin.Code, http.StatusOK)
	}

	deleteResponse := performJSONRequest(t, server, http.MethodDelete, "/api/users/"+createdUser.ID, nil, cookies)
	if deleteResponse.Code != http.StatusNoContent {
		t.Fatalf("DELETE /api/users/{id} status = %d, want %d", deleteResponse.Code, http.StatusNoContent)
	}

	usersResponse := performJSONRequest(t, server, http.MethodGet, "/api/users", nil, cookies)
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

	server := mustNew(t, Options{
		LoginTimingFloor: -1,
		Now:              func() time.Time { return now },
		Store:            store,
	})
	defer server.Close()
	adminUser, _, err := server.auth.BootstrapUser(context.Background(), auth.BootstrapInput{
		Username: "admin",
		Password: "Admin1password",
		Role:     auth.RoleAdmin,
	}, now)
	if err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}

	loginResponse := performJSONRequest(t, server, http.MethodPost, "/api/auth/login", map[string]string{
		"username": "admin",
		"password": "Admin1password",
	}, nil)
	if loginResponse.Code != http.StatusOK {
		t.Fatalf("POST /api/auth/login status = %d, want %d", loginResponse.Code, http.StatusOK)
	}
	cookies := loginResponse.Result().Cookies()

	deleteResponse := performJSONRequest(t, server, http.MethodDelete, "/api/users/"+adminUser.ID, nil, cookies)
	if deleteResponse.Code != http.StatusBadRequest {
		t.Fatalf("DELETE /api/users/self status = %d, want %d", deleteResponse.Code, http.StatusBadRequest)
	}

	demoteResponse := performJSONRequest(t, server, http.MethodPut, "/api/users/"+adminUser.ID, map[string]string{
		"username": adminUser.Username,
		"role":     "viewer",
	}, cookies)
	if demoteResponse.Code != http.StatusBadRequest {
		t.Fatalf("PUT /api/users/{id} last admin demotion status = %d, want %d", demoteResponse.Code, http.StatusBadRequest)
	}
}

// S-5: PUT /api/users/{id} self-edit must reject a password change with no
// current_password (400) and a wrong current_password (401), and must accept
// the right one (200). The caller's session cookie must remain usable across
// all three calls — none of the failure paths revoke sessions, and the
// success path explicitly preserves the calling session.
func TestHTTPUsersSelfPasswordChangeRequiresCurrentPassword(t *testing.T) {
	now := time.Date(2026, time.March, 16, 22, 0, 0, 0, time.UTC)
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	server := mustNew(t, Options{
		LoginTimingFloor: -1,
		Now:              func() time.Time { return now },
		Store:            store,
	})
	defer server.Close()

	adminUser, _, err := server.auth.BootstrapUser(context.Background(), auth.BootstrapInput{
		Username: "admin",
		Password: "Admin1password",
		Role:     auth.RoleAdmin,
	}, now)
	if err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}
	// A second admin so the role-validation path stays valid even if the
	// caller tweaks role; not strictly required for this test, but it
	// guards against the test breaking if upstream policy gets stricter.
	if _, _, err := server.auth.BootstrapUser(context.Background(), auth.BootstrapInput{
		Username: "admin2",
		Password: "Admin2password",
		Role:     auth.RoleAdmin,
	}, now); err != nil {
		t.Fatalf("BootstrapUser(admin2) error = %v", err)
	}

	loginResponse := performJSONRequest(t, server, http.MethodPost, "/api/auth/login", map[string]string{
		"username": "admin",
		"password": "Admin1password",
	}, nil)
	if loginResponse.Code != http.StatusOK {
		t.Fatalf("POST /api/auth/login status = %d, want %d", loginResponse.Code, http.StatusOK)
	}
	cookies := loginResponse.Result().Cookies()

	// (a) self-edit, missing current_password -> 400.
	missingResponse := performJSONRequest(t, server, http.MethodPut, "/api/users/"+adminUser.ID, map[string]string{
		"username":     "admin",
		"role":         "admin",
		"new_password": "RotatedAdmin1pwd",
	}, cookies)
	if missingResponse.Code != http.StatusBadRequest {
		t.Fatalf("PUT /api/users/self missing current_password status = %d, want %d", missingResponse.Code, http.StatusBadRequest)
	}

	// (b) self-edit, wrong current_password -> 401.
	wrongResponse := performJSONRequest(t, server, http.MethodPut, "/api/users/"+adminUser.ID, map[string]string{
		"username":         "admin",
		"role":             "admin",
		"new_password":     "RotatedAdmin1pwd",
		"current_password": "WrongOriginal1pwd",
	}, cookies)
	if wrongResponse.Code != http.StatusUnauthorized {
		t.Fatalf("PUT /api/users/self wrong current_password status = %d, want %d", wrongResponse.Code, http.StatusUnauthorized)
	}

	// Old password must still authenticate — the failures above cannot
	// have rotated state.
	stillOldLogin := performJSONRequest(t, server, http.MethodPost, "/api/auth/login", map[string]string{
		"username": "admin",
		"password": "Admin1password",
	}, nil)
	if stillOldLogin.Code != http.StatusOK {
		t.Fatalf("POST /api/auth/login old password after failed self-edit status = %d, want %d", stillOldLogin.Code, http.StatusOK)
	}

	// (c) self-edit, correct current_password -> 200.
	okResponse := performJSONRequest(t, server, http.MethodPut, "/api/users/"+adminUser.ID, map[string]string{
		"username":         "admin",
		"role":             "admin",
		"new_password":     "RotatedAdmin1pwd",
		"current_password": "Admin1password",
	}, cookies)
	if okResponse.Code != http.StatusOK {
		t.Fatalf("PUT /api/users/self correct current_password status = %d, want %d", okResponse.Code, http.StatusOK)
	}

	// Caller session must still work after a successful self-edit
	// (ExceptSessionID preserved it). Listing users requires admin role
	// and a live session; if either is gone this returns non-200.
	stillAuthorized := performJSONRequest(t, server, http.MethodGet, "/api/users", nil, cookies)
	if stillAuthorized.Code != http.StatusOK {
		t.Fatalf("GET /api/users with caller cookies after self-edit status = %d, want %d", stillAuthorized.Code, http.StatusOK)
	}

	// New password works on a fresh login; old one no longer does.
	newLogin := performJSONRequest(t, server, http.MethodPost, "/api/auth/login", map[string]string{
		"username": "admin",
		"password": "RotatedAdmin1pwd",
	}, nil)
	if newLogin.Code != http.StatusOK {
		t.Fatalf("POST /api/auth/login new password status = %d, want %d", newLogin.Code, http.StatusOK)
	}
	rejectedLogin := performJSONRequest(t, server, http.MethodPost, "/api/auth/login", map[string]string{
		"username": "admin",
		"password": "Admin1password",
	}, nil)
	if rejectedLogin.Code == http.StatusOK {
		t.Fatalf("POST /api/auth/login old password after rotation status = %d, want non-OK", rejectedLogin.Code)
	}
}

// S-5: when an admin rotates a *different* user's password, every session
// belonging to that target must be invalidated. The handler does not require
// the target's current password (admin role is the trusted authority); any
// outstanding cookie the target held is revoked at the auth-service layer.
func TestHTTPUsersAdminPasswordChangeRevokesTargetSessions(t *testing.T) {
	now := time.Date(2026, time.March, 16, 22, 30, 0, 0, time.UTC)
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	server := mustNew(t, Options{
		LoginTimingFloor: -1,
		Now:              func() time.Time { return now },
		Store:            store,
	})
	defer server.Close()

	if _, _, err := server.auth.BootstrapUser(context.Background(), auth.BootstrapInput{
		Username: "admin",
		Password: "Admin1password",
		Role:     auth.RoleAdmin,
	}, now); err != nil {
		t.Fatalf("BootstrapUser(admin) error = %v", err)
	}
	target, _, err := server.auth.BootstrapUser(context.Background(), auth.BootstrapInput{
		Username: "operator",
		Password: "Operator1password",
		Role:     auth.RoleOperator,
	}, now)
	if err != nil {
		t.Fatalf("BootstrapUser(target) error = %v", err)
	}

	adminLogin := performJSONRequest(t, server, http.MethodPost, "/api/auth/login", map[string]string{
		"username": "admin",
		"password": "Admin1password",
	}, nil)
	if adminLogin.Code != http.StatusOK {
		t.Fatalf("admin login status = %d, want %d", adminLogin.Code, http.StatusOK)
	}
	adminCookies := adminLogin.Result().Cookies()

	targetLogin := performJSONRequest(t, server, http.MethodPost, "/api/auth/login", map[string]string{
		"username": "operator",
		"password": "Operator1password",
	}, nil)
	if targetLogin.Code != http.StatusOK {
		t.Fatalf("target login status = %d, want %d", targetLogin.Code, http.StatusOK)
	}
	targetCookies := targetLogin.Result().Cookies()

	// Sanity: target's cookie reaches at least one authorised endpoint
	// before the rotation. /api/auth/me is the canonical "am I logged in"
	// probe; we settle for any 2xx.
	preCheck := performJSONRequest(t, server, http.MethodGet, "/api/auth/me", nil, targetCookies)
	if preCheck.Code/100 != 2 {
		t.Fatalf("GET /api/auth/me target before rotation status = %d, want 2xx", preCheck.Code)
	}

	rotate := performJSONRequest(t, server, http.MethodPut, "/api/users/"+target.ID, map[string]string{
		"username":     "operator",
		"role":         "operator",
		"new_password": "AdminRotated1pass",
	}, adminCookies)
	if rotate.Code != http.StatusOK {
		t.Fatalf("PUT /api/users/{target} status = %d, want %d", rotate.Code, http.StatusOK)
	}

	// Target's cookie must no longer be valid — every session for that
	// user was revoked, including this one.
	postCheck := performJSONRequest(t, server, http.MethodGet, "/api/auth/me", nil, targetCookies)
	if postCheck.Code == http.StatusOK {
		t.Fatalf("GET /api/auth/me target after rotation status = %d, want session revoked", postCheck.Code)
	}
}
