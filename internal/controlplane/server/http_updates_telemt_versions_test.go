package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/lost-coder/panvex/internal/controlplane/storage/sqlite"
	"github.com/lost-coder/panvex/internal/controlplane/updates"
)

// TestMergeUpdateSettingsClampsTelemtVersionsToShow pins the Task 2 merge
// contract: an out-of-range value from the PUT request body is clamped
// (not rejected) via updates.ClampTelemtVersionsToShow, and a nil field
// (omitted from the request) leaves the current persisted value untouched —
// mirroring how every other *int field in mergeUpdateSettings behaves.
func TestMergeUpdateSettingsClampsTelemtVersionsToShow(t *testing.T) {
	current := UpdateSettings{TelemtVersionsToShow: 5}
	over := 50
	mergeUpdateSettings(&current, updateSettingsRequest{TelemtVersionsToShow: &over})
	if current.TelemtVersionsToShow != 20 {
		t.Fatalf("TelemtVersionsToShow = %d, want clamped 20", current.TelemtVersionsToShow)
	}

	unchanged := UpdateSettings{TelemtVersionsToShow: 7}
	mergeUpdateSettings(&unchanged, updateSettingsRequest{})
	if unchanged.TelemtVersionsToShow != 7 {
		t.Fatalf("nil field must leave current value untouched, got %d", unchanged.TelemtVersionsToShow)
	}
}

// TestHandleGetUpdateSettingsDefaultTelemtVersionsToShow pins the Task 2 GET
// contract: a fresh server (no settings ever persisted) surfaces the
// updates.DefaultTelemtVersionsToShow default under
// settings.telemt_versions_to_show, so the dashboard's version-picker depth
// control has a sane starting value instead of 0.
func TestHandleGetUpdateSettingsDefaultTelemtVersionsToShow(t *testing.T) {
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	server := mustNew(t, Options{LoginTimingFloor: -1, Store: store})
	defer server.Close()

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/settings/updates", nil)
	server.handleGetUpdateSettings().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var body struct {
		Settings struct {
			TelemtVersionsToShow int `json:"telemt_versions_to_show"`
		} `json:"settings"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if body.Settings.TelemtVersionsToShow != updates.DefaultTelemtVersionsToShow {
		t.Fatalf("telemt_versions_to_show = %d, want default %d", body.Settings.TelemtVersionsToShow, updates.DefaultTelemtVersionsToShow)
	}
}
