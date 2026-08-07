package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/lost-coder/panvex/internal/controlplane/storage/sqlite"
	"github.com/lost-coder/panvex/internal/controlplane/updates"
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

// TestHandleGetUpdateSettingsCarriesSelfUpdatePhase pins the R11b Task 3
// contract: GET /settings/updates surfaces the persisted self-update lifecycle
// under "self_update", populated from updates.Service.LoadSelfUpdate, so the
// dashboard reads an in-flight/terminal phase from the server (surviving a page
// reload) instead of only from an ephemeral React mutation flag.
func TestHandleGetUpdateSettingsCarriesSelfUpdatePhase(t *testing.T) {
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	server := mustNew(t, Options{LoginTimingFloor: -1, Store: store})
	defer server.Close()

	want := updates.SelfUpdateState{
		Phase:       updates.SelfUpdateDownloading,
		FromVersion: "1.0.0",
		ToVersion:   "1.1.0",
		Message:     "",
		UpdatedAt:   1_700_000_000,
	}
	if err := server.updatesSvc.SaveSelfUpdate(context.Background(), want); err != nil {
		t.Fatalf("SaveSelfUpdate() error = %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/settings/updates", nil)
	server.handleGetUpdateSettings().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var body struct {
		SelfUpdate updates.SelfUpdateState `json:"self_update"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if body.SelfUpdate != want {
		t.Fatalf("self_update = %+v, want %+v; body=%s", body.SelfUpdate, want, rec.Body.String())
	}
}

// TestHandleGetUpdateSettingsCarriesTelemtReleaseState pins the Task 10
// (Telemt Update v1) contract: GET /settings/updates surfaces the cached
// Telemt release-check fields nested under "state" alongside the existing
// panel/agent fields, so the dashboard can read them without a new endpoint.
func TestHandleGetUpdateSettingsCarriesTelemtReleaseState(t *testing.T) {
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	server := mustNew(t, Options{LoginTimingFloor: -1, Store: store})
	defer server.Close()

	server.settingsMu.Lock()
	server.updateState = UpdateState{
		LatestPanelVersion:   "1.2.3",
		TelemtLatestVersion:  "3.4.25",
		TelemtReleaseBaseURL: "https://github.com/telemt/telemt/releases/download/3.4.25",
		TelemtLastCheckError: "",
	}
	server.settingsMu.Unlock()

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/settings/updates", nil)
	server.handleGetUpdateSettings().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var body struct {
		State UpdateState `json:"state"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if body.State.TelemtLatestVersion != "3.4.25" {
		t.Fatalf("state.telemt_latest_version = %q, want %q; body=%s", body.State.TelemtLatestVersion, "3.4.25", rec.Body.String())
	}
	if body.State.TelemtReleaseBaseURL != "https://github.com/telemt/telemt/releases/download/3.4.25" {
		t.Fatalf("state.telemt_release_base_url = %q; body=%s", body.State.TelemtReleaseBaseURL, rec.Body.String())
	}
	// The panel/agent fields must still be present — Task 10 must not have
	// dropped the pre-existing contract.
	if body.State.LatestPanelVersion != "1.2.3" {
		t.Fatalf("state.latest_panel_version = %q, want %q; body=%s", body.State.LatestPanelVersion, "1.2.3", rec.Body.String())
	}
}

// TestCheckTelemtReleaseIndependentOfPanelAgentCheck pins the "same tick,
// separate try" contract from Task 10: a Telemt check must never touch the
// panel/agent-owned fields on State, and vice versa — checkPanelAndAgentUpdates
// must never touch the Telemt-owned fields. Exercised directly through
// updates.ApplyTelemtCheckResult (the function checkTelemtRelease delegates
// to) since checkForUpdates itself talks to the real GitHub API and has no
// test seam for that.
func TestCheckTelemtReleaseIndependentOfPanelAgentCheck(t *testing.T) {
	state := UpdateState{
		LatestPanelVersion:   "1.2.3",
		LatestAgentVersion:   "1.2.3",
		LastCheckError:       "",
		TelemtLatestVersion:  "3.4.20",
		TelemtReleaseBaseURL: "https://github.com/telemt/telemt/releases/download/3.4.20",
	}

	// A failing Telemt check must not disturb the panel/agent fields.
	updates.ApplyTelemtCheckResult(&state, nil, nil, errors.New("github api returned status 403: rate limit exceeded"))
	if state.LatestPanelVersion != "1.2.3" || state.LatestAgentVersion != "1.2.3" {
		t.Fatalf("panel/agent fields disturbed by a failing Telemt check: %+v", state)
	}
	if state.TelemtLastCheckError == "" {
		t.Fatal("TelemtLastCheckError not set after a failing Telemt check")
	}
	// The stale version must be preserved on failure.
	if state.TelemtLatestVersion != "3.4.20" {
		t.Fatalf("TelemtLatestVersion = %q, want preserved %q", state.TelemtLatestVersion, "3.4.20")
	}
}
