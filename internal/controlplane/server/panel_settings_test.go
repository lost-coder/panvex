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

// TestPublicURLChangeReachesAgentURL is the end-to-end regression: a public_url
// saved via the store must reach the agent install URL builder.
func TestPublicURLChangeReachesAgentURL(t *testing.T) {
	srv := testServerWithSQLite(t, time.Date(2026, time.May, 28, 10, 0, 0, 0, time.UTC))
	ctx := context.Background()
	if err := srv.settings.Put(ctx, map[string]string{"http.public_url": "https://newpanel.example"}, "test"); err != nil {
		t.Fatalf("Put error = %v", err)
	}
	// panelRuntime is the zero value here (AgentHTTPRootPath/HTTPRootPath empty),
	// so buildAgentPublicURL returns the bare base URL.
	got := buildAgentPublicURL(srv.panelSettingsSnapshot(), srv.panelRuntime, nil, "", "")
	if got != "https://newpanel.example" {
		t.Fatalf("buildAgentPublicURL = %q, want %q", got, "https://newpanel.example")
	}
}

// TestPanelRestartStatusReflectsPendingChanges pins Plan 5 Task 3:
// panelRestartStatus().Pending must reflect real pending restart-tier changes
// (via PendingChanges against the captured-active snapshot) instead of the
// previously hardcoded false.
func TestPanelRestartStatusReflectsPendingChanges(t *testing.T) {
	srv := testServerWithSQLite(t, time.Now())
	ctx := context.Background()
	// Change a restart-tier operational setting away from its captured-active
	// value (default 30m).
	if err := srv.settings.Put(ctx, map[string]string{"auth.session_idle_timeout": "45m"}, "test"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if !srv.panelRestartStatus().Pending {
		t.Fatal("panelRestartStatus().Pending = false, want true after a restart-tier change")
	}
}
