package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/lost-coder/panvex/internal/controlplane/storage/sqlite"
)

// TestHandleGetUpdateSettingsReturnsNestedSettings pins the wire shape of
// GET /settings/updates to the nested {settings:{…}, state, current_version}
// form the dashboard's updateSettingsResponseSchema parses. A flat response
// (settings fields at the top level) makes the Zod parse throw, which the
// UpdatesSettingsSection turns into `return null` — the whole Updates panel
// silently vanishes. This test is the contract guard that was missing.
func TestHandleGetUpdateSettingsReturnsNestedSettings(t *testing.T) {
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	server := mustNew(t, Options{LoginTimingFloor: -1, Store: store})
	defer server.Close()

	server.settingsMu.Lock()
	server.updateSettings = UpdateSettings{
		CheckIntervalHours:  6,
		GitHubRepo:          "lost-coder/panvex",
		AgentDownloadSource: "github",
	}
	server.updateState = UpdateState{LatestPanelVersion: "1.2.3"}
	server.settingsMu.Unlock()

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/settings/updates", nil)
	server.handleGetUpdateSettings().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var body map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}

	for _, key := range []string{"settings", "state", "current_version"} {
		if _, ok := body[key]; !ok {
			t.Fatalf("response missing top-level %q; body=%s", key, rec.Body.String())
		}
	}
	// Settings must be nested, not promoted to the top level.
	if _, leaked := body["check_interval_hours"]; leaked {
		t.Fatalf("settings fields leaked to top level; body=%s", rec.Body.String())
	}

	var settings struct {
		CheckIntervalHours  int    `json:"check_interval_hours"`
		GitHubRepo          string `json:"github_repo"`
		AgentDownloadSource string `json:"agent_download_source"`
	}
	if err := json.Unmarshal(body["settings"], &settings); err != nil {
		t.Fatalf("unmarshal settings: %v", err)
	}
	if settings.CheckIntervalHours != 6 || settings.GitHubRepo != "lost-coder/panvex" || settings.AgentDownloadSource != "github" {
		t.Fatalf("nested settings = %+v, want seeded values", settings)
	}
}
