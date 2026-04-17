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

// TestRetentionSettingsSurviveRestart covers P2-REL-03 / finding M-R1:
// retention settings must be persisted to the storage layer so that a
// control-plane restart picks them up instead of silently reverting to
// defaults. The test performs a PUT, creates a fresh server instance on
// the same on-disk sqlite database (simulating a restart), and asserts
// the GET returns the written values.
func TestRetentionSettingsSurviveRestart(t *testing.T) {
	now := time.Date(2026, time.April, 17, 10, 0, 0, 0, time.UTC)
	dbPath := filepath.Join(t.TempDir(), "panvex.db")

	store, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	// Don't defer Close here — we explicitly close & reopen below to
	// simulate a process restart.

	server := New(Options{
		Now:   func() time.Time { return now },
		Store: store,
	})
	if _, _, err := server.auth.BootstrapUser(auth.BootstrapInput{
		Username: "admin",
		Password: "Admin1password",
		Role:     auth.RoleAdmin,
	}, now); err != nil {
		t.Fatalf("BootstrapUser(admin) error = %v", err)
	}

	adminLogin := performJSONRequest(t, server.Handler(), http.MethodPost, "/api/auth/login", map[string]string{
		"username": "admin",
		"password": "Admin1password",
	}, nil)
	if adminLogin.Code != http.StatusOK {
		t.Fatalf("POST /api/auth/login admin status = %d, want %d", adminLogin.Code, http.StatusOK)
	}
	cookies := adminLogin.Result().Cookies()

	// A fresh server uses defaults — confirm before mutating anything.
	initial := performJSONRequest(t, server.Handler(), http.MethodGet, "/api/settings/retention", nil, cookies)
	if initial.Code != http.StatusOK {
		t.Fatalf("GET /api/settings/retention (initial) status = %d, want %d", initial.Code, http.StatusOK)
	}
	var initialPayload RetentionSettings
	if err := json.Unmarshal(initial.Body.Bytes(), &initialPayload); err != nil {
		t.Fatalf("json.Unmarshal(initial) error = %v", err)
	}
	if initialPayload != defaultRetentionSettings() {
		t.Fatalf("initial retention = %+v, want defaults %+v", initialPayload, defaultRetentionSettings())
	}

	// Operator overrides the retention windows.
	desired := RetentionSettings{
		TSRawSeconds:     3600,
		TSHourlySeconds:  7200,
		TSDCSeconds:      1800,
		IPHistorySeconds: 604800,
		EventSeconds:     900,
	}
	putResp := performJSONRequest(t, server.Handler(), http.MethodPut, "/api/settings/retention", desired, cookies)
	if putResp.Code != http.StatusOK {
		t.Fatalf("PUT /api/settings/retention status = %d, want %d; body = %s", putResp.Code, http.StatusOK, putResp.Body.String())
	}
	var putPayload RetentionSettings
	if err := json.Unmarshal(putResp.Body.Bytes(), &putPayload); err != nil {
		t.Fatalf("json.Unmarshal(put) error = %v", err)
	}
	if putPayload != desired {
		t.Fatalf("PUT response = %+v, want %+v", putPayload, desired)
	}

	// Simulate a restart: close the first server+store, reopen the store
	// against the same DB file, and build a brand-new server. If
	// retention is not persisted, New() will fall back to defaults and
	// the assertion below fails.
	server.Close()
	if err := store.Close(); err != nil {
		t.Fatalf("store.Close() error = %v", err)
	}

	restartedStore, err := sqlite.Open(dbPath)
	if err != nil {
		t.Fatalf("sqlite.Open() (restart) error = %v", err)
	}
	defer restartedStore.Close()

	restartedServer := New(Options{
		Now:   func() time.Time { return now },
		Store: restartedStore,
	})
	defer restartedServer.Close()

	// In-memory snapshot must reflect the persisted blob without any
	// HTTP round-trip — restoreRetentionSettings() runs inside New().
	if got := restartedServer.retentionSettings(); got != desired {
		t.Fatalf("restartedServer.retentionSettings() = %+v, want %+v", got, desired)
	}

	// And the HTTP surface must agree. Bootstrap the admin on the new
	// process (a real restart re-runs the same seed step) and log in.
	if _, _, err := restartedServer.auth.BootstrapUser(auth.BootstrapInput{
		Username: "admin",
		Password: "Admin1password",
		Role:     auth.RoleAdmin,
	}, now); err != nil {
		// BootstrapUser is idempotent for an existing admin — ignore
		// the duplicate-admin error path; the log-in below is the real
		// signal.
		t.Logf("BootstrapUser(restart) returned %v (acceptable if admin already exists)", err)
	}
	restartLogin := performJSONRequest(t, restartedServer.Handler(), http.MethodPost, "/api/auth/login", map[string]string{
		"username": "admin",
		"password": "Admin1password",
	}, nil)
	if restartLogin.Code != http.StatusOK {
		t.Fatalf("POST /api/auth/login after restart status = %d, want %d", restartLogin.Code, http.StatusOK)
	}
	getResp := performJSONRequest(t, restartedServer.Handler(), http.MethodGet, "/api/settings/retention", nil, restartLogin.Result().Cookies())
	if getResp.Code != http.StatusOK {
		t.Fatalf("GET /api/settings/retention after restart status = %d, want %d", getResp.Code, http.StatusOK)
	}
	var getPayload RetentionSettings
	if err := json.Unmarshal(getResp.Body.Bytes(), &getPayload); err != nil {
		t.Fatalf("json.Unmarshal(get after restart) error = %v", err)
	}
	if getPayload != desired {
		t.Fatalf("GET after restart = %+v, want %+v", getPayload, desired)
	}
}
