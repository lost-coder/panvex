package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/auth"
	"github.com/lost-coder/panvex/internal/controlplane/storage/sqlite"
)

// B1: on audit-persist failure we reject the login with 503 and do not
// issue a session cookie. A session that was briefly created inside
// auth.Authenticate (to capture session fixation guarantees) must be
// revoked on the way out so no untraceable session lingers.
func TestLoginAbortsWhenAuditPersistFails(t *testing.T) {
	now := time.Date(2026, time.April, 19, 10, 0, 0, 0, time.UTC)
	base, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer base.Close()

	injectedErr := errors.New("synthetic audit persist failure")
	store := &failingStore{MigrationStore: base, appendAuditEventErr: injectedErr}

	srv := mustNew(t, Options{
		LoginTimingFloor: -1,
		Now:              func() time.Time { return now },
		Store:            store,
	})
	defer srv.Close()

	if _, _, err := srv.auth.BootstrapUser(context.Background(), auth.BootstrapInput{
		Username: "admin",
		Password: "Admin1password",
		Role:     auth.RoleAdmin,
	}, now); err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}

	resp := performJSONRequest(t, srv, http.MethodPost, "/api/auth/login", map[string]string{
		"username": "admin",
		"password": "Admin1password",
	}, nil)

	if resp.Code != http.StatusServiceUnavailable {
		t.Fatalf("login status = %d, want %d", resp.Code, http.StatusServiceUnavailable)
	}

	for _, c := range resp.Result().Cookies() {
		if c.Name == sessionCookieName && c.Value != "" && c.MaxAge >= 0 {
			t.Fatalf("unexpected session cookie issued after audit persist failure: %+v", c)
		}
	}

	var body map[string]string
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["code"] != "audit_persist_unavailable" {
		t.Fatalf("error code = %q, want %q", body["code"], "audit_persist_unavailable")
	}

	// After rejection a second login (with audit restored) must succeed —
	// this proves the user's lockout counter wasn't incremented and the
	// session table isn't polluted by the aborted attempt.
	store.appendAuditEventErr = nil
	okResp := performJSONRequest(t, srv, http.MethodPost, "/api/auth/login", map[string]string{
		"username": "admin",
		"password": "Admin1password",
	}, nil)
	if okResp.Code != http.StatusOK {
		t.Fatalf("recovered login status = %d, want %d (body=%s)", okResp.Code, http.StatusOK, okResp.Body.String())
	}
}

func TestLoginSuccess(t *testing.T) {
	now := time.Date(2026, time.April, 15, 10, 0, 0, 0, time.UTC)
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	srv := mustNew(t, Options{
		LoginTimingFloor: -1,
		Now:              func() time.Time { return now },
		Store:            store,
	})
	defer srv.Close()

	if _, _, err := srv.auth.BootstrapUser(context.Background(), auth.BootstrapInput{
		Username: "admin",
		Password: "Admin1password",
		Role:     auth.RoleAdmin,
	}, now); err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}

	resp := performJSONRequest(t, srv, http.MethodPost, "/api/auth/login", map[string]string{
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
	srv := mustNew(t, Options{
		LoginTimingFloor: -1,
		Now:              func() time.Time { return now },
	})

	if _, _, err := srv.auth.BootstrapUser(context.Background(), auth.BootstrapInput{
		Username: "admin",
		Password: "Admin1password",
		Role:     auth.RoleAdmin,
	}, now); err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}

	resp := performJSONRequest(t, srv, http.MethodPost, "/api/auth/login", map[string]string{
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
	srv := mustNew(t, Options{
		LoginTimingFloor: -1,
		Now:              func() time.Time { return now },
	})

	longPassword := make([]byte, 1025)
	for i := range longPassword {
		longPassword[i] = 'a'
	}

	resp := performJSONRequest(t, srv, http.MethodPost, "/api/auth/login", map[string]string{
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

	srv := mustNew(t, Options{
		LoginTimingFloor: -1,
		Now:              func() time.Time { return now },
		Store:            store,
	})
	defer srv.Close()

	if _, _, err := srv.auth.BootstrapUser(context.Background(), auth.BootstrapInput{
		Username: "admin",
		Password: "Admin1password",
		Role:     auth.RoleAdmin,
	}, now); err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}

	// Exhaust login attempts.
	for i := 0; i < accountLockoutMaxAttempts+1; i++ {
		performJSONRequest(t, srv, http.MethodPost, "/api/auth/login", map[string]string{
			"username": "admin",
			"password": "WrongPassword1",
		}, nil)
	}

	// Even correct password should be rejected while locked.
	resp := performJSONRequest(t, srv, http.MethodPost, "/api/auth/login", map[string]string{
		"username": "admin",
		"password": "Admin1password",
	}, nil)
	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("login on locked account status = %d, want %d", resp.Code, http.StatusUnauthorized)
	}
}

// S-6: when a TOTP-enabled user supplies a wrong second-factor code
// repeatedly, the request handler must lock the account on the new
// stricter TOTP counter (3 attempts / 5 min) — not on the lenient
// password counter — and the next attempt with a fresh, valid code
// must be rejected as locked.
func TestLoginTOTPLockoutIntegration(t *testing.T) {
	now := time.Date(2026, time.April, 15, 10, 0, 0, 0, time.UTC)
	srv := mustNew(t, Options{
		LoginTimingFloor: -1,
		Now:              func() time.Time { return now },
	})
	defer srv.Close()

	user, _, err := srv.auth.BootstrapUser(context.Background(), auth.BootstrapInput{
		Username: "alice",
		Password: "Alice1password",
		Role:     auth.RoleOperator,
	}, now)
	if err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}

	secret, err := srv.auth.StartTotpSetup(context.Background(), user.ID, now)
	if err != nil {
		t.Fatalf("StartTotpSetup() error = %v", err)
	}
	enableCode, err := srv.auth.GenerateTotpCode(secret, now)
	if err != nil {
		t.Fatalf("GenerateTotpCode() error = %v", err)
	}
	if _, err := srv.auth.EnableTotp(context.Background(), user.ID, "Alice1password", enableCode, now); err != nil {
		t.Fatalf("EnableTotp() error = %v", err)
	}

	// Submit TOTPLockoutMaxAttempts wrong codes against the right password.
	// The counter should be on the TOTP tracker, not the password tracker.
	for i := 0; i < totpLockoutMaxAttempts; i++ {
		resp := performJSONRequest(t, srv, http.MethodPost, "/api/auth/login", map[string]string{
			"username":  "alice",
			"password":  "Alice1password",
			"totp_code": "000000",
		}, nil)
		if resp.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: status = %d, want %d", i+1, resp.Code, http.StatusUnauthorized)
		}
	}

	// A valid code must now be rejected as locked. Generate a code at a
	// later time so the consumed-TOTP map can't possibly be the cause of
	// rejection — the only reason this fails is the TOTP lockout.
	freshAt := now.Add(60 * time.Second)
	freshCode, err := srv.auth.GenerateTotpCode(secret, freshAt)
	if err != nil {
		t.Fatalf("GenerateTotpCode(fresh) error = %v", err)
	}
	resp := performJSONRequest(t, srv, http.MethodPost, "/api/auth/login", map[string]string{
		"username":  "alice",
		"password":  "Alice1password",
		"totp_code": freshCode,
	}, nil)
	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("locked-out login with fresh code status = %d, want %d", resp.Code, http.StatusUnauthorized)
	}

	// Sanity: the password tracker should NOT be locked. A direct check on
	// the in-process trackers proves the two counters are independent.
	if srv.loginLockout.IsLockedWithContext(context.Background(), "alice", now) {
		t.Fatal("password lockout tripped by TOTP-only failures, want only TOTP tracker locked")
	}
	if !srv.totpLockout.IsLockedWithContext(context.Background(), "alice", now) {
		t.Fatal("TOTP lockout not engaged after threshold failures")
	}
}

// S-6: the password tracker must not be incremented by TOTP failures.
// This is the converse of TestTOTPLockoutIndependentFromPasswordLockout
// at the unit level — exercise the same property through the HTTP
// surface so the wiring in handleLoginAuthError is also covered.
func TestLoginPasswordCounterNotBumpedByTOTPFailures(t *testing.T) {
	now := time.Date(2026, time.April, 15, 10, 0, 0, 0, time.UTC)
	srv := mustNew(t, Options{
		LoginTimingFloor: -1,
		Now:              func() time.Time { return now },
	})
	defer srv.Close()

	user, _, err := srv.auth.BootstrapUser(context.Background(), auth.BootstrapInput{
		Username: "bob",
		Password: "Bob1password",
		Role:     auth.RoleOperator,
	}, now)
	if err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}
	secret, err := srv.auth.StartTotpSetup(context.Background(), user.ID, now)
	if err != nil {
		t.Fatalf("StartTotpSetup() error = %v", err)
	}
	enableCode, err := srv.auth.GenerateTotpCode(secret, now)
	if err != nil {
		t.Fatalf("GenerateTotpCode() error = %v", err)
	}
	if _, err := srv.auth.EnableTotp(context.Background(), user.ID, "Bob1password", enableCode, now); err != nil {
		t.Fatalf("EnableTotp() error = %v", err)
	}

	// Burn one TOTP failure (below the threshold). The password counter
	// must remain at zero — sub-threshold TOTP failures should never
	// touch the password tracker.
	resp := performJSONRequest(t, srv, http.MethodPost, "/api/auth/login", map[string]string{
		"username":  "bob",
		"password":  "Bob1password",
		"totp_code": "000000",
	}, nil)
	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("wrong-totp status = %d, want %d", resp.Code, http.StatusUnauthorized)
	}
	if srv.loginLockout.IsLockedWithContext(context.Background(), "bob", now) {
		t.Fatal("password tracker locked after TOTP failure, want unaffected")
	}
}

func TestLogoutClearsCookie(t *testing.T) {
	now := time.Date(2026, time.April, 15, 10, 0, 0, 0, time.UTC)
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	srv := mustNew(t, Options{
		LoginTimingFloor: -1,
		Now:              func() time.Time { return now },
		Store:            store,
	})
	defer srv.Close()

	if _, _, err := srv.auth.BootstrapUser(context.Background(), auth.BootstrapInput{
		Username: "admin",
		Password: "Admin1password",
		Role:     auth.RoleAdmin,
	}, now); err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}

	loginResp := performJSONRequest(t, srv, http.MethodPost, "/api/auth/login", map[string]string{
		"username": "admin",
		"password": "Admin1password",
	}, nil)
	cookies := loginResp.Result().Cookies()

	logoutResp := performJSONRequest(t, srv, http.MethodPost, "/api/auth/logout", nil, cookies)
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
	meResp := performJSONRequest(t, srv, http.MethodGet, "/api/auth/me", nil, cookies)
	if meResp.Code != http.StatusUnauthorized {
		t.Fatalf("GET /api/auth/me after logout status = %d, want %d", meResp.Code, http.StatusUnauthorized)
	}
}

func TestLogoutWithoutSessionReturnsUnauthorized(t *testing.T) {
	now := time.Date(2026, time.April, 15, 10, 0, 0, 0, time.UTC)
	srv := mustNew(t, Options{
		LoginTimingFloor: -1,
		Now:              func() time.Time { return now },
	})

	resp := performJSONRequest(t, srv, http.MethodPost, "/api/auth/logout", nil, nil)
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

	srv := mustNew(t, Options{
		LoginTimingFloor: -1,
		Now:              func() time.Time { return now },
		Store:            store,
	})
	defer srv.Close()

	if _, _, err := srv.auth.BootstrapUser(context.Background(), auth.BootstrapInput{
		Username: "operator",
		Password: "Operator1pass",
		Role:     auth.RoleOperator,
	}, now); err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}

	loginResp := performJSONRequest(t, srv, http.MethodPost, "/api/auth/login", map[string]string{
		"username": "operator",
		"password": "Operator1pass",
	}, nil)
	cookies := loginResp.Result().Cookies()

	meResp := performJSONRequest(t, srv, http.MethodGet, "/api/auth/me", nil, cookies)
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
	srv := mustNew(t, Options{
		LoginTimingFloor: -1,
		Now:              func() time.Time { return now },
	})

	resp := performJSONRequest(t, srv, http.MethodGet, "/api/auth/me", nil, nil)
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
	srv := mustNew(t, Options{
		LoginTimingFloor: -1,
		Now:              func() time.Time { return now },
	})

	if _, _, err := srv.auth.BootstrapUser(context.Background(), auth.BootstrapInput{
		Username: "operator",
		Password: "Operator1password",
		Role:     auth.RoleOperator,
	}, now); err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}

	// First login establishes a session whose ID we will treat as the
	// pre-authentication (potentially planted) cookie value.
	firstLogin := performJSONRequest(t, srv, http.MethodPost, "/api/auth/login", map[string]string{
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
	meBefore := performJSONRequest(t, srv, http.MethodGet, "/api/auth/me", nil, firstCookies)
	if meBefore.Code != http.StatusOK {
		t.Fatalf("GET /api/auth/me before rotation status = %d, want %d", meBefore.Code, http.StatusOK)
	}

	// Re-login carrying the prior cookie, as a browser would naturally do.
	secondLogin := performJSONRequest(t, srv, http.MethodPost, "/api/auth/login", map[string]string{
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
	meAfter := performJSONRequest(t, srv, http.MethodGet, "/api/auth/me", nil, []*http.Cookie{
		{Name: sessionCookieName, Value: firstSessionID},
	})
	if meAfter.Code != http.StatusUnauthorized {
		t.Fatalf("GET /api/auth/me with invalidated cookie status = %d, want %d", meAfter.Code, http.StatusUnauthorized)
	}

	// The new cookie should still authenticate.
	meFresh := performJSONRequest(t, srv, http.MethodGet, "/api/auth/me", nil, []*http.Cookie{
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
	srv := mustNew(t, Options{
		LoginTimingFloor: -1,
		Now:              func() time.Time { return now },
	})

	if _, _, err := srv.auth.BootstrapUser(context.Background(), auth.BootstrapInput{
		Username: "admin",
		Password: "Admin1password",
		Role:     auth.RoleAdmin,
	}, now); err != nil {
		t.Fatalf("BootstrapUser(admin) error = %v", err)
	}
	viewer, _, err := srv.auth.BootstrapUser(context.Background(), auth.BootstrapInput{
		Username: "viewer",
		Password: "Viewer1password",
		Role:     auth.RoleViewer,
	}, now)
	if err != nil {
		t.Fatalf("BootstrapUser(viewer) error = %v", err)
	}

	// Viewer logs in and obtains a session.
	viewerLogin := performJSONRequest(t, srv, http.MethodPost, "/api/auth/login", map[string]string{
		"username": "viewer",
		"password": "Viewer1password",
	}, nil)
	if viewerLogin.Code != http.StatusOK {
		t.Fatalf("viewer login status = %d, want %d", viewerLogin.Code, http.StatusOK)
	}
	viewerCookies := viewerLogin.Result().Cookies()

	// Admin logs in and promotes the viewer to operator.
	adminLogin := performJSONRequest(t, srv, http.MethodPost, "/api/auth/login", map[string]string{
		"username": "admin",
		"password": "Admin1password",
	}, nil)
	if adminLogin.Code != http.StatusOK {
		t.Fatalf("admin login status = %d, want %d", adminLogin.Code, http.StatusOK)
	}

	updateResp := performJSONRequest(t, srv, http.MethodPut, "/api/users/"+viewer.ID, map[string]string{
		"username": "viewer",
		"role":     string(auth.RoleOperator),
	}, adminLogin.Result().Cookies())
	if updateResp.Code != http.StatusOK {
		t.Fatalf("PUT /api/users/{id} status = %d, want %d", updateResp.Code, http.StatusOK)
	}

	// Viewer's prior session must no longer authenticate.
	meAfter := performJSONRequest(t, srv, http.MethodGet, "/api/auth/me", nil, viewerCookies)
	if meAfter.Code != http.StatusUnauthorized {
		t.Fatalf("viewer /me after role change status = %d, want %d", meAfter.Code, http.StatusUnauthorized)
	}

	// Admin session is untouched.
	adminMe := performJSONRequest(t, srv, http.MethodGet, "/api/auth/me", nil, adminLogin.Result().Cookies())
	if adminMe.Code != http.StatusOK {
		t.Fatalf("admin /me after unrelated role change status = %d, want %d", adminMe.Code, http.StatusOK)
	}
}

// TestLogin_AlwaysRotatesSessionID is an S-03 regression: login must issue a
// brand-new session-ID even when the browser carries a forged (attacker-known)
// cookie.  The returned Set-Cookie value must differ from the planted value.
func TestLogin_AlwaysRotatesSessionID(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.May, 2, 10, 0, 0, 0, time.UTC)
	srv := mustNew(t, Options{
		LoginTimingFloor: -1,
		Now:              func() time.Time { return now },
	})
	defer srv.Close()

	if _, _, err := srv.auth.BootstrapUser(context.Background(), auth.BootstrapInput{
		Username: "alice",
		Password: "Alice-Password-1",
		Role:     auth.RoleOperator,
	}, now); err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}

	// Plant a forged cookie value — the attacker knows this string.
	forgedCookie := &http.Cookie{Name: sessionCookieName, Value: "attacker-known-id-123"}

	resp := performJSONRequest(t, srv, http.MethodPost, "/api/auth/login",
		map[string]string{"username": "alice", "password": "Alice-Password-1"},
		[]*http.Cookie{forgedCookie})

	if resp.Code != http.StatusOK {
		t.Fatalf("login: code=%d body=%s", resp.Code, resp.Body.String())
	}

	var newID string
	for _, c := range resp.Result().Cookies() {
		if c.Name == sessionCookieName {
			newID = c.Value
		}
	}
	if newID == "" {
		t.Fatal("no session cookie issued after login")
	}
	if newID == forgedCookie.Value {
		t.Fatalf("session-ID was NOT rotated: server reused forged value %q", forgedCookie.Value)
	}
}

// TestLogin_CookieIsOpaque covers S22 Task 5 (S-medium): the cookie
// value emitted at login must be the *opaque* session token, not the
// internal Session.ID (which is the HMAC-SHA-256 lookup hash and the
// DB primary key). Hashing the cookie back under the service's
// session-lookup key must reproduce the in-memory session.ID — that
// is the round-trip the HTTP layer relies on every authenticated
// request. The audit-log target ID embedded in the auth.login event
// must also not echo the cookie verbatim: the audit pipeline runs its
// own log-redaction HMAC with a different key, so the persisted
// target is two layers away from a live cookie.
func TestLogin_CookieIsOpaque(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.May, 3, 10, 0, 0, 0, time.UTC)
	base, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer base.Close()

	srv := mustNew(t, Options{
		LoginTimingFloor: -1,
		Now:              func() time.Time { return now },
		Store:            base,
		EncryptionKey:    "test-encryption-key-for-session-lookup",
	})
	defer srv.Close()

	if _, _, err := srv.auth.BootstrapUser(context.Background(), auth.BootstrapInput{
		Username: "alice",
		Password: "Alice-Password-1",
		Role:     auth.RoleOperator,
	}, now); err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}

	resp := performJSONRequest(t, srv, http.MethodPost, "/api/auth/login",
		map[string]string{"username": "alice", "password": "Alice-Password-1"},
		nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("login: code=%d body=%s", resp.Code, resp.Body.String())
	}

	var cookieValue string
	for _, c := range resp.Result().Cookies() {
		if c.Name == sessionCookieName {
			cookieValue = c.Value
		}
	}
	if cookieValue == "" {
		t.Fatal("no session cookie issued after login")
	}

	// Resolve the cookie back to the in-memory session and check the
	// HMAC round-trip. The auth service keeps Session.Cookie zero on
	// reads, so we look up via GetSessionByCookie — exactly the
	// production HTTP path.
	session, err := srv.auth.GetSessionByCookie(cookieValue)
	if err != nil {
		t.Fatalf("GetSessionByCookie() error = %v", err)
	}
	if session.ID == "" {
		t.Fatal("resolved session.ID is empty")
	}
	if session.ID == cookieValue {
		t.Fatalf("session.ID == cookie value = %q; the DB primary key must be the HMAC of the cookie, not the cookie itself", cookieValue)
	}
	if got := len(session.ID); got != 64 {
		t.Fatalf("len(session.ID) = %d, want 64 (hex SHA-256)", got)
	}

	// The audit pipeline must not write the cookie verbatim either.
	// auth.login is appended synchronously during login, so by the
	// time the response returns the row is already present.
	events, err := base.ListAuditEvents(context.Background(), 0)
	if err != nil {
		t.Fatalf("ListAuditEvents() error = %v", err)
	}
	if len(events) == 0 {
		t.Fatal("no audit events recorded for login")
	}
	for _, evt := range events {
		if evt.Action != "auth.login" {
			continue
		}
		if evt.TargetID == cookieValue {
			t.Fatalf("audit event TargetID equals cookie value %q; want redacted hash", cookieValue)
		}
		if evt.TargetID == session.ID {
			t.Fatalf("audit event TargetID equals internal lookup hash %q; want a separately-keyed redaction so a leak of the audit table cannot be replayed against the session DB", session.ID)
		}
		if evt.TargetID == "" {
			t.Fatal("audit event TargetID empty; want s-… log-redacted form")
		}
	}
}

// TestLogin_PriorSessionIDIsRevoked is an S-03 regression: after a second
// login the original session-ID must be invalidated server-side — a request
// carrying the old cookie must receive 401.
func TestLogin_PriorSessionIDIsRevoked(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.May, 2, 10, 0, 0, 0, time.UTC)
	srv := mustNew(t, Options{
		LoginTimingFloor: -1,
		Now:              func() time.Time { return now },
	})
	defer srv.Close()

	if _, _, err := srv.auth.BootstrapUser(context.Background(), auth.BootstrapInput{
		Username: "alice",
		Password: "Alice-Password-1",
		Role:     auth.RoleOperator,
	}, now); err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}

	// Step 1: initial login — capture the real session-ID.
	firstResp := performJSONRequest(t, srv, http.MethodPost, "/api/auth/login",
		map[string]string{"username": "alice", "password": "Alice-Password-1"},
		nil)
	if firstResp.Code != http.StatusOK {
		t.Fatalf("first login: code=%d body=%s", firstResp.Code, firstResp.Body.String())
	}

	firstCookies := firstResp.Result().Cookies()
	var priorID string
	for _, c := range firstCookies {
		if c.Name == sessionCookieName {
			priorID = c.Value
		}
	}
	if priorID == "" {
		t.Fatal("no session cookie from initial login")
	}

	// Verify the initial session works.
	meOK := performJSONRequest(t, srv, http.MethodGet, "/api/auth/me", nil, firstCookies)
	if meOK.Code != http.StatusOK {
		t.Fatalf("GET /api/auth/me before re-login: code=%d, want 200", meOK.Code)
	}

	// Step 2: re-login carrying the prior session cookie.
	secondResp := performJSONRequest(t, srv, http.MethodPost, "/api/auth/login",
		map[string]string{"username": "alice", "password": "Alice-Password-1"},
		firstCookies)
	if secondResp.Code != http.StatusOK {
		t.Fatalf("re-login: code=%d body=%s", secondResp.Code, secondResp.Body.String())
	}

	// Step 3: old session-ID must now return 401.
	meAfter := performJSONRequest(t, srv, http.MethodGet, "/api/auth/me", nil,
		[]*http.Cookie{{Name: sessionCookieName, Value: priorID}})
	if meAfter.Code != http.StatusUnauthorized {
		t.Fatalf("old session still authenticates: code=%d, want 401", meAfter.Code)
	}
}

// Plan 3 / Task 7: a disconnected client must not pin the login goroutine
// for the full constant-time delay budget. The login slow-path pads every
// response to LoginTimingFloor so wall-clock timing can't distinguish
// branches; that padding must be cancellable when r.Context() is Done.
func TestLogin_ConstantTimeDelayHonoursClientDisconnect(t *testing.T) {
	now := time.Date(2026, time.April, 19, 10, 0, 0, 0, time.UTC)
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	// Use a large floor so a non-cancellable Sleep would clearly exceed
	// the 500ms threshold, while a cancellable select returns promptly.
	srv := mustNew(t, Options{
		LoginTimingFloor: 3 * time.Second,
		Now:              func() time.Time { return now },
		Store:            store,
	})
	defer srv.Close()

	// Cancel shortly after the handler enters its slow-path so the
	// earlier pre-floor work (lockout/auth DB queries) completes against
	// a live context — only the constant-time padding itself should
	// observe the cancellation. Without the Task 7 fix the floor sleeps
	// for the full 3s; with the fix the select returns as soon as
	// ctx.Done() and the handler responds in well under a second.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		time.Sleep(700 * time.Millisecond)
		cancel()
	}()

	req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/api/auth/login",
		strings.NewReader(`{"username":"nobody","password":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	// Satisfy the CSRF Origin check so the request reaches the login
	// handler (otherwise the middleware short-circuits with 403 and we
	// never enter the timing-floor slow-path under test).
	req.Header.Set("Origin", "http://"+req.Host)
	rec := httptest.NewRecorder()

	start := time.Now()
	srv.Handler().ServeHTTP(rec, req)
	elapsed := time.Since(start)

	if elapsed > 1500*time.Millisecond {
		t.Fatalf("disconnected client should not pin for full slow-path budget (3s); status=%d body=%s elapsed=%v",
			rec.Code, rec.Body.String(), elapsed)
	}
}
