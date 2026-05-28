package server

import (
	"context"
	"testing"
	"time"
)

// TestPanelSettingsSnapshotReadsLiveStore pins Plan 3: panelSettingsSnapshot()
// must read the live OperationalStore so a value saved through
// /api/settings/values (which writes only the store) propagates immediately to
// enrollment/auth consumers without a /api/settings/panel write or restart.
func TestPanelSettingsSnapshotReadsLiveStore(t *testing.T) {
	srv := testServerWithSQLite(t, time.Date(2026, time.May, 28, 10, 0, 0, 0, time.UTC))
	ctx := context.Background()
	if err := srv.settings.Put(ctx, map[string]string{"http.public_url": "https://changed.example"}, "test"); err != nil {
		t.Fatalf("Put error = %v", err)
	}
	if got := srv.panelSettingsSnapshot().HTTPPublicURL; got != "https://changed.example" {
		t.Fatalf("panelSettingsSnapshot().HTTPPublicURL = %q, want %q", got, "https://changed.example")
	}
}
