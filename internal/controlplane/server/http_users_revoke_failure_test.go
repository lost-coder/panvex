package server

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/auth"
	"github.com/lost-coder/panvex/internal/controlplane/storage"
	"github.com/lost-coder/panvex/internal/controlplane/storage/sqlite"
)

// alwaysFailSessionStore satisfies auth.SessionStore but refuses every
// DeleteSession call. Used to exercise the HTTP-layer surface of PVX-002:
// PUT /api/users/{id} must report a 500 (not 200) when the credential
// rotation's mandatory session revocation cannot be persisted, and must
// leave the stored credential untouched.
type alwaysFailSessionStore struct{}

func (alwaysFailSessionStore) PutSession(context.Context, storage.SessionRecord) error {
	return nil
}

func (alwaysFailSessionStore) DeleteSession(context.Context, string) error {
	return errSimulatedSessionStoreOutage
}

func (alwaysFailSessionStore) ListSessions(context.Context) ([]storage.SessionRecord, error) {
	return nil, nil
}

func (alwaysFailSessionStore) DeleteExpiredSessions(context.Context, time.Time) error {
	return nil
}

func (alwaysFailSessionStore) TouchSession(context.Context, string, time.Time) error {
	return nil
}

var errSimulatedSessionStoreOutage = errors.New("simulated session store outage")

// TestHTTPUpdateUserPasswordChangeReturns500WhenSessionRevocationFails
// verifies the HTTP surface of D2/D3: when an admin rotates another user's
// password, UpdateUser must revoke that user's sessions BEFORE persisting
// the new hash. If the store refuses the session delete, the handler must
// answer 500 (never 200 — reporting success while a supposedly-revoked
// session survives is exactly the fail-open defect PVX-002 fixes), and the
// old password must still authenticate.
func TestHTTPUpdateUserPasswordChangeReturns500WhenSessionRevocationFails(t *testing.T) {
	now := time.Date(2026, time.July, 17, 12, 0, 0, 0, time.UTC)
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

	// Give the target an active session on the healthy store before we
	// swap in the failing one, so the revocation attempt has a real row to
	// (fail to) delete.
	targetLogin := performJSONRequest(t, server, http.MethodPost, "/api/auth/login", map[string]string{
		"username": "operator",
		"password": "Operator1password",
	}, nil)
	if targetLogin.Code != http.StatusOK {
		t.Fatalf("target login status = %d, want %d", targetLogin.Code, http.StatusOK)
	}
	targetCookies := targetLogin.Result().Cookies()

	// Swap in a store that refuses every DeleteSession from here on.
	server.auth.SetSessionStore(alwaysFailSessionStore{})

	rotate := performJSONRequest(t, server, http.MethodPut, "/api/users/"+target.ID, map[string]string{
		"username":     "operator",
		"role":         "operator",
		"new_password": "AdminRotated1pass",
	}, adminCookies)
	if rotate.Code != http.StatusInternalServerError {
		t.Fatalf("PUT /api/users/{target} status = %d, want %d (revocation failure must fail closed)", rotate.Code, http.StatusInternalServerError)
	}

	// The credential must be untouched: the OLD password still logs in.
	oldLogin := performJSONRequest(t, server, http.MethodPost, "/api/auth/login", map[string]string{
		"username": "operator",
		"password": "Operator1password",
	}, nil)
	if oldLogin.Code != http.StatusOK {
		t.Fatalf("POST /api/auth/login old password after failed rotation status = %d, want %d", oldLogin.Code, http.StatusOK)
	}

	// The new password must NOT work — it was never persisted.
	newLogin := performJSONRequest(t, server, http.MethodPost, "/api/auth/login", map[string]string{
		"username": "operator",
		"password": "AdminRotated1pass",
	}, nil)
	if newLogin.Code == http.StatusOK {
		t.Fatal("POST /api/auth/login new password after failed rotation status = 200, want rejected (credential must not change)")
	}

	// The target's pre-existing session cookie must remain usable — the
	// operation aborted before it could touch anything durably.
	stillAuthorized := performJSONRequest(t, server, http.MethodGet, "/api/auth/me", nil, targetCookies)
	if stillAuthorized.Code/100 != 2 {
		t.Fatalf("GET /api/auth/me target after failed rotation status = %d, want 2xx (session must survive an aborted update)", stillAuthorized.Code)
	}
}
