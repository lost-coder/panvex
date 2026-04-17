package server

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/auth"
	"github.com/lost-coder/panvex/internal/controlplane/storage/sqlite"
)

func TestLoginSuccess(t *testing.T) {
	now := time.Date(2026, time.April, 15, 10, 0, 0, 0, time.UTC)
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	srv := New(Options{
		Now:   func() time.Time { return now },
		Store: store,
	})
	defer srv.Close()

	if _, _, err := srv.auth.BootstrapUser(auth.BootstrapInput{
		Username: "admin",
		Password: "Admin1password",
		Role:     auth.RoleAdmin,
	}, now); err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}

	resp := performJSONRequest(t, srv.Handler(), http.MethodPost, "/api/auth/login", map[string]string{
		"username": "admin",
		"password": "Admin1password",
	}, nil)

	if resp.Code != http.StatusOK {
		t.Fatalf("login status = %d, want %d", resp.Code, http.StatusOK)
	}

	cookies := resp.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("login returned no session cookie")
	}

	found := false
	for _, c := range cookies {
		if c.Name == sessionCookieName {
			found = true
			if !c.HttpOnly {
				t.Fatal("session cookie HttpOnly = false, want true")
			}
		}
	}
	if !found {
		t.Fatalf("session cookie %q not found in response", sessionCookieName)
	}
}

func TestLoginInvalidCredentials(t *testing.T) {
	now := time.Date(2026, time.April, 15, 10, 0, 0, 0, time.UTC)
	srv := New(Options{
		Now: func() time.Time { return now },
	})

	if _, _, err := srv.auth.BootstrapUser(auth.BootstrapInput{
		Username: "admin",
		Password: "Admin1password",
		Role:     auth.RoleAdmin,
	}, now); err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}

	resp := performJSONRequest(t, srv.Handler(), http.MethodPost, "/api/auth/login", map[string]string{
		"username": "admin",
		"password": "WrongPassword1",
	}, nil)

	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("login wrong password status = %d, want %d", resp.Code, http.StatusUnauthorized)
	}

	var body struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err == nil {
		if body.Code != "invalid_credentials" {
			t.Fatalf("error code = %q, want %q", body.Code, "invalid_credentials")
		}
	}
}

func TestLoginPasswordExceedsMaxLength(t *testing.T) {
	now := time.Date(2026, time.April, 15, 10, 0, 0, 0, time.UTC)
	srv := New(Options{
		Now: func() time.Time { return now },
	})

	longPassword := make([]byte, 1025)
	for i := range longPassword {
		longPassword[i] = 'a'
	}

	resp := performJSONRequest(t, srv.Handler(), http.MethodPost, "/api/auth/login", map[string]string{
		"username": "admin",
		"password": string(longPassword),
	}, nil)

	if resp.Code != http.StatusBadRequest {
		t.Fatalf("login oversized password status = %d, want %d", resp.Code, http.StatusBadRequest)
	}
}

func TestLoginLockoutIntegration(t *testing.T) {
	now := time.Date(2026, time.April, 15, 10, 0, 0, 0, time.UTC)
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	srv := New(Options{
		Now:   func() time.Time { return now },
		Store: store,
	})
	defer srv.Close()

	if _, _, err := srv.auth.BootstrapUser(auth.BootstrapInput{
		Username: "admin",
		Password: "Admin1password",
		Role:     auth.RoleAdmin,
	}, now); err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}

	// Exhaust login attempts.
	for i := 0; i < accountLockoutMaxAttempts+1; i++ {
		performJSONRequest(t, srv.Handler(), http.MethodPost, "/api/auth/login", map[string]string{
			"username": "admin",
			"password": "WrongPassword1",
		}, nil)
	}

	// Even correct password should be rejected while locked.
	resp := performJSONRequest(t, srv.Handler(), http.MethodPost, "/api/auth/login", map[string]string{
		"username": "admin",
		"password": "Admin1password",
	}, nil)
	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("login on locked account status = %d, want %d", resp.Code, http.StatusUnauthorized)
	}
}

func TestLogoutClearsCookie(t *testing.T) {
	now := time.Date(2026, time.April, 15, 10, 0, 0, 0, time.UTC)
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	srv := New(Options{
		Now:   func() time.Time { return now },
		Store: store,
	})
	defer srv.Close()

	if _, _, err := srv.auth.BootstrapUser(auth.BootstrapInput{
		Username: "admin",
		Password: "Admin1password",
		Role:     auth.RoleAdmin,
	}, now); err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}

	loginResp := performJSONRequest(t, srv.Handler(), http.MethodPost, "/api/auth/login", map[string]string{
		"username": "admin",
		"password": "Admin1password",
	}, nil)
	cookies := loginResp.Result().Cookies()

	logoutResp := performJSONRequest(t, srv.Handler(), http.MethodPost, "/api/auth/logout", nil, cookies)
	if logoutResp.Code != http.StatusNoContent {
		t.Fatalf("logout status = %d, want %d", logoutResp.Code, http.StatusNoContent)
	}

	// Cookie should be cleared (MaxAge = -1).
	for _, c := range logoutResp.Result().Cookies() {
		if c.Name == sessionCookieName && c.MaxAge != -1 {
			t.Fatalf("logout cookie MaxAge = %d, want -1", c.MaxAge)
		}
	}

	// Session should be invalidated — /me should fail.
	meResp := performJSONRequest(t, srv.Handler(), http.MethodGet, "/api/auth/me", nil, cookies)
	if meResp.Code != http.StatusUnauthorized {
		t.Fatalf("GET /api/auth/me after logout status = %d, want %d", meResp.Code, http.StatusUnauthorized)
	}
}

func TestLogoutWithoutSessionReturnsUnauthorized(t *testing.T) {
	now := time.Date(2026, time.April, 15, 10, 0, 0, 0, time.UTC)
	srv := New(Options{
		Now: func() time.Time { return now },
	})

	resp := performJSONRequest(t, srv.Handler(), http.MethodPost, "/api/auth/logout", nil, nil)
	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("logout without session status = %d, want %d", resp.Code, http.StatusUnauthorized)
	}
}

func TestMeReturnsUserInfo(t *testing.T) {
	now := time.Date(2026, time.April, 15, 10, 0, 0, 0, time.UTC)
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	srv := New(Options{
		Now:   func() time.Time { return now },
		Store: store,
	})
	defer srv.Close()

	if _, _, err := srv.auth.BootstrapUser(auth.BootstrapInput{
		Username: "operator",
		Password: "Operator1pass",
		Role:     auth.RoleOperator,
	}, now); err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}

	loginResp := performJSONRequest(t, srv.Handler(), http.MethodPost, "/api/auth/login", map[string]string{
		"username": "operator",
		"password": "Operator1pass",
	}, nil)
	cookies := loginResp.Result().Cookies()

	meResp := performJSONRequest(t, srv.Handler(), http.MethodGet, "/api/auth/me", nil, cookies)
	if meResp.Code != http.StatusOK {
		t.Fatalf("GET /api/auth/me status = %d, want %d", meResp.Code, http.StatusOK)
	}

	var payload meResponse
	if err := json.Unmarshal(meResp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if payload.Username != "operator" {
		t.Fatalf("username = %q, want %q", payload.Username, "operator")
	}
	if payload.Role != "operator" {
		t.Fatalf("role = %q, want %q", payload.Role, "operator")
	}
	if payload.TotpEnabled {
		t.Fatal("totp_enabled = true, want false")
	}
	if payload.ID == "" {
		t.Fatal("id = empty, want non-empty")
	}
}

func TestMeWithoutSessionReturnsUnauthorized(t *testing.T) {
	now := time.Date(2026, time.April, 15, 10, 0, 0, 0, time.UTC)
	srv := New(Options{
		Now: func() time.Time { return now },
	})

	resp := performJSONRequest(t, srv.Handler(), http.MethodGet, "/api/auth/me", nil, nil)
	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("GET /api/auth/me without session status = %d, want %d", resp.Code, http.StatusUnauthorized)
	}
}

func TestBuildTotpAuthURL(t *testing.T) {
	url := buildTotpAuthURL("alice", "JBSWY3DPEHPK3PXP")
	expected := "otpauth://totp/Panvex:alice?secret=JBSWY3DPEHPK3PXP&issuer=Panvex"
	if url != expected {
		t.Fatalf("buildTotpAuthURL() = %q, want %q", url, expected)
	}
}

func TestBuildTotpAuthURLEscapesSpecialChars(t *testing.T) {
	url := buildTotpAuthURL("user with spaces", "SECRET+KEY")
	if url == "" {
		t.Fatal("buildTotpAuthURL() returned empty string")
	}
	// Should contain URL-encoded spaces in username.
	if !containsSubstring(url, "user%20with%20spaces") && !containsSubstring(url, "user+with+spaces") {
		t.Fatalf("buildTotpAuthURL() did not escape username: %q", url)
	}
}

func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) && searchSubstring(s, substr)
}

func searchSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// TestLoginRotatesSessionIDOnExistingCookie covers P2-SEC-01 at the HTTP
// layer: if the browser submits a login request while already carrying a
// panvex_session cookie, that pre-authentication session must be invalidated
// server-side. The new cookie returned in the response must have a different
// value, and the old value must no longer authenticate subsequent requests.
func TestLoginRotatesSessionIDOnExistingCookie(t *testing.T) {
	now := time.Date(2026, time.April, 15, 10, 0, 0, 0, time.UTC)
	srv := New(Options{
		Now: func() time.Time { return now },
	})

	if _, _, err := srv.auth.BootstrapUser(auth.BootstrapInput{
		Username: "operator",
		Password: "Operator1password",
		Role:     auth.RoleOperator,
	}, now); err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}

	// First login establishes a session whose ID we will treat as the
	// pre-authentication (potentially planted) cookie value.
	firstLogin := performJSONRequest(t, srv.Handler(), http.MethodPost, "/api/auth/login", map[string]string{
		"username": "operator",
		"password": "Operator1password",
	}, nil)
	if firstLogin.Code != http.StatusOK {
		t.Fatalf("first POST /api/auth/login status = %d, want %d", firstLogin.Code, http.StatusOK)
	}

	firstCookies := firstLogin.Result().Cookies()
	var firstSessionID string
	for _, c := range firstCookies {
		if c.Name == sessionCookieName {
			firstSessionID = c.Value
		}
	}
	if firstSessionID == "" {
		t.Fatal("first login returned no session cookie")
	}

	// Confirm the first session authenticates /me before we re-login.
	meBefore := performJSONRequest(t, srv.Handler(), http.MethodGet, "/api/auth/me", nil, firstCookies)
	if meBefore.Code != http.StatusOK {
		t.Fatalf("GET /api/auth/me before rotation status = %d, want %d", meBefore.Code, http.StatusOK)
	}

	// Re-login carrying the prior cookie, as a browser would naturally do.
	secondLogin := performJSONRequest(t, srv.Handler(), http.MethodPost, "/api/auth/login", map[string]string{
		"username": "operator",
		"password": "Operator1password",
	}, firstCookies)
	if secondLogin.Code != http.StatusOK {
		t.Fatalf("second POST /api/auth/login status = %d, want %d", secondLogin.Code, http.StatusOK)
	}

	secondCookies := secondLogin.Result().Cookies()
	var secondSessionID string
	for _, c := range secondCookies {
		if c.Name == sessionCookieName {
			secondSessionID = c.Value
		}
	}
	if secondSessionID == "" {
		t.Fatal("second login returned no session cookie")
	}
	if secondSessionID == firstSessionID {
		t.Fatal("second login session ID matches first; want rotated ID")
	}

	// The prior cookie must no longer authenticate. Submit /me with only the
	// old cookie value to isolate that effect.
	meAfter := performJSONRequest(t, srv.Handler(), http.MethodGet, "/api/auth/me", nil, []*http.Cookie{
		{Name: sessionCookieName, Value: firstSessionID},
	})
	if meAfter.Code != http.StatusUnauthorized {
		t.Fatalf("GET /api/auth/me with invalidated cookie status = %d, want %d", meAfter.Code, http.StatusUnauthorized)
	}

	// The new cookie should still authenticate.
	meFresh := performJSONRequest(t, srv.Handler(), http.MethodGet, "/api/auth/me", nil, []*http.Cookie{
		{Name: sessionCookieName, Value: secondSessionID},
	})
	if meFresh.Code != http.StatusOK {
		t.Fatalf("GET /api/auth/me with rotated cookie status = %d, want %d", meFresh.Code, http.StatusOK)
	}
}

// TestRoleChangeInvalidatesTargetUserSessions covers the privilege-change
// rotation half of P2-SEC-01: when an admin edits another user's role, that
// user's outstanding sessions must immediately stop authenticating so the
// target re-authenticates under the new privilege level.
func TestRoleChangeInvalidatesTargetUserSessions(t *testing.T) {
	now := time.Date(2026, time.April, 15, 10, 0, 0, 0, time.UTC)
	srv := New(Options{
		Now: func() time.Time { return now },
	})

	if _, _, err := srv.auth.BootstrapUser(auth.BootstrapInput{
		Username: "admin",
		Password: "Admin1password",
		Role:     auth.RoleAdmin,
	}, now); err != nil {
		t.Fatalf("BootstrapUser(admin) error = %v", err)
	}
	viewer, _, err := srv.auth.BootstrapUser(auth.BootstrapInput{
		Username: "viewer",
		Password: "Viewer1password",
		Role:     auth.RoleViewer,
	}, now)
	if err != nil {
		t.Fatalf("BootstrapUser(viewer) error = %v", err)
	}

	// Viewer logs in and obtains a session.
	viewerLogin := performJSONRequest(t, srv.Handler(), http.MethodPost, "/api/auth/login", map[string]string{
		"username": "viewer",
		"password": "Viewer1password",
	}, nil)
	if viewerLogin.Code != http.StatusOK {
		t.Fatalf("viewer login status = %d, want %d", viewerLogin.Code, http.StatusOK)
	}
	viewerCookies := viewerLogin.Result().Cookies()

	// Admin logs in and promotes the viewer to operator.
	adminLogin := performJSONRequest(t, srv.Handler(), http.MethodPost, "/api/auth/login", map[string]string{
		"username": "admin",
		"password": "Admin1password",
	}, nil)
	if adminLogin.Code != http.StatusOK {
		t.Fatalf("admin login status = %d, want %d", adminLogin.Code, http.StatusOK)
	}

	updateResp := performJSONRequest(t, srv.Handler(), http.MethodPut, "/api/users/"+viewer.ID, map[string]string{
		"username": "viewer",
		"role":     string(auth.RoleOperator),
	}, adminLogin.Result().Cookies())
	if updateResp.Code != http.StatusOK {
		t.Fatalf("PUT /api/users/{id} status = %d, want %d", updateResp.Code, http.StatusOK)
	}

	// Viewer's prior session must no longer authenticate.
	meAfter := performJSONRequest(t, srv.Handler(), http.MethodGet, "/api/auth/me", nil, viewerCookies)
	if meAfter.Code != http.StatusUnauthorized {
		t.Fatalf("viewer /me after role change status = %d, want %d", meAfter.Code, http.StatusUnauthorized)
	}

	// Admin session is untouched.
	adminMe := performJSONRequest(t, srv.Handler(), http.MethodGet, "/api/auth/me", nil, adminLogin.Result().Cookies())
	if adminMe.Code != http.StatusOK {
		t.Fatalf("admin /me after unrelated role change status = %d, want %d", adminMe.Code, http.StatusOK)
	}
}
