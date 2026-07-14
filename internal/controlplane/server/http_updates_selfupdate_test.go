package server

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/auth"
	"github.com/lost-coder/panvex/internal/controlplane/storage/sqlite"
	"github.com/lost-coder/panvex/internal/controlplane/updates"
)

// TestPerformPanelUpdateChecksumFailureSetsFailedPhase covers R11b Task 2
// Step 1(a): a checksum-fetch failure must leave a persisted, terminal
// "failed" phase (with a stage-specific message) instead of silently
// vanishing — the previous behaviour before this task.
func TestPerformPanelUpdateChecksumFailureSetsFailedPhase(t *testing.T) {
	now := time.Date(2026, time.July, 15, 10, 0, 0, 0, time.UTC)
	server := testServerWithSQLite(t, now)
	server.version = "1.0.0"
	server.selfUpdateChecksumFetcher = func(ctx context.Context, checksumURL, token string) (string, bool) {
		return "", false
	}

	server.performPanelUpdate("user-1", "1.1.0", "https://example.invalid/download", "https://example.invalid/checksum", "")

	st, err := server.updatesSvc.LoadSelfUpdate(context.Background())
	if err != nil {
		t.Fatalf("LoadSelfUpdate() error = %v", err)
	}
	if st.Phase != updates.SelfUpdateFailed {
		t.Fatalf("Phase = %q, want %q", st.Phase, updates.SelfUpdateFailed)
	}
	if st.Message != "checksum fetch failed" {
		t.Fatalf("Message = %q, want %q", st.Message, "checksum fetch failed")
	}
	if st.FromVersion != "1.0.0" || st.ToVersion != "1.1.0" {
		t.Fatalf("From/To = %q/%q, want 1.0.0/1.1.0", st.FromVersion, st.ToVersion)
	}
}

// TestPerformPanelUpdateSuccessfulInstallSetsRestartPending covers Step
// 1(b): a successful download+install with no restart hook wired (the
// common case when RequestRestart is unset, e.g. no supervisor) must leave
// the terminal-eventually "restart_pending" phase with the documented
// operator-facing message.
func TestPerformPanelUpdateSuccessfulInstallSetsRestartPending(t *testing.T) {
	now := time.Date(2026, time.July, 15, 10, 0, 0, 0, time.UTC)
	server := testServerWithSQLite(t, now)
	server.version = "1.0.0"
	server.selfUpdateChecksumFetcher = func(ctx context.Context, checksumURL, token string) (string, bool) {
		return "deadbeef", true
	}
	server.selfUpdateArchiveDownloader = func(ctx context.Context, downloadURL, expectedChecksum, token string) (string, bool) {
		return filepath.Join(t.TempDir(), "fake-archive.tar.gz"), true
	}
	server.selfUpdateInstaller = func(ctx context.Context, archivePath string) bool {
		return true
	}
	server.requestRestart = nil

	server.performPanelUpdate("user-1", "1.1.0", "https://example.invalid/download", "https://example.invalid/checksum", "")

	st, err := server.updatesSvc.LoadSelfUpdate(context.Background())
	if err != nil {
		t.Fatalf("LoadSelfUpdate() error = %v", err)
	}
	if st.Phase != updates.SelfUpdateRestartPending {
		t.Fatalf("Phase = %q, want %q", st.Phase, updates.SelfUpdateRestartPending)
	}
	if st.Message != "binary staged; waiting for supervisor restart" {
		t.Fatalf("Message = %q, want the restart-pending message", st.Message)
	}
}

// TestPerformPanelUpdateRestartRequestFailureDowngradesToFailed covers the
// plan's explicit "binary prepared, manual restart required" outcome: the
// install succeeds (phase optimistically set to restart_pending) but the
// restart hook itself errors, which must downgrade the phase to the
// terminal "failed" with the operator-actionable message from the plan.
func TestPerformPanelUpdateRestartRequestFailureDowngradesToFailed(t *testing.T) {
	now := time.Date(2026, time.July, 15, 10, 0, 0, 0, time.UTC)
	server := testServerWithSQLite(t, now)
	server.version = "1.0.0"
	server.selfUpdateChecksumFetcher = func(ctx context.Context, checksumURL, token string) (string, bool) {
		return "deadbeef", true
	}
	server.selfUpdateArchiveDownloader = func(ctx context.Context, downloadURL, expectedChecksum, token string) (string, bool) {
		return filepath.Join(t.TempDir(), "fake-archive.tar.gz"), true
	}
	server.selfUpdateInstaller = func(ctx context.Context, archivePath string) bool {
		return true
	}
	server.requestRestart = func() error { return errors.New("supervisor unreachable") }

	server.performPanelUpdate("user-1", "1.1.0", "https://example.invalid/download", "https://example.invalid/checksum", "")

	st, err := server.updatesSvc.LoadSelfUpdate(context.Background())
	if err != nil {
		t.Fatalf("LoadSelfUpdate() error = %v", err)
	}
	if st.Phase != updates.SelfUpdateFailed {
		t.Fatalf("Phase = %q, want %q", st.Phase, updates.SelfUpdateFailed)
	}
	const wantMessage = "binary staged, but restart could not be requested; restart the panel manually to finish the update"
	if st.Message != wantMessage {
		t.Fatalf("Message = %q, want %q", st.Message, wantMessage)
	}
}

// TestFinalizeSelfUpdateStateBootCompletesWhenVersionMatches covers Step
// 1(c): a restart_pending phase left behind by a crash/restart, observed at
// the next boot with the running version already at the recorded target,
// must finalise to "completed" (the restart is itself the proof the new
// binary is live).
func TestFinalizeSelfUpdateStateBootCompletesWhenVersionMatches(t *testing.T) {
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	svc := updates.NewService(store)
	if err := svc.SaveSelfUpdate(context.Background(), updates.SelfUpdateState{
		Phase:       updates.SelfUpdateRestartPending,
		FromVersion: "1.0.0",
		ToVersion:   "1.1.0",
	}); err != nil {
		t.Fatalf("SaveSelfUpdate() error = %v", err)
	}

	server := mustNew(t, Options{LoginTimingFloor: -1, Store: store, Version: "1.1.0"})
	defer server.Close()

	st, err := server.updatesSvc.LoadSelfUpdate(context.Background())
	if err != nil {
		t.Fatalf("LoadSelfUpdate() error = %v", err)
	}
	if st.Phase != updates.SelfUpdateCompleted {
		t.Fatalf("Phase = %q, want %q", st.Phase, updates.SelfUpdateCompleted)
	}
	if st.Message != "" {
		t.Fatalf("Message = %q, want empty on completion", st.Message)
	}
}

// TestFinalizeSelfUpdateStateBootFailsWhenVersionBehindTarget covers Step
// 1(d): the same interrupted restart_pending phase, but the panel came back
// up on the OLD binary (install never took effect, or the process rolled
// back) — must finalise to "failed" with the plan's exact message so the
// operator knows to retry.
func TestFinalizeSelfUpdateStateBootFailsWhenVersionBehindTarget(t *testing.T) {
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	svc := updates.NewService(store)
	if err := svc.SaveSelfUpdate(context.Background(), updates.SelfUpdateState{
		Phase:       updates.SelfUpdateRestartPending,
		FromVersion: "1.0.0",
		ToVersion:   "1.1.0",
	}); err != nil {
		t.Fatalf("SaveSelfUpdate() error = %v", err)
	}

	server := mustNew(t, Options{LoginTimingFloor: -1, Store: store, Version: "1.0.0"})
	defer server.Close()

	st, err := server.updatesSvc.LoadSelfUpdate(context.Background())
	if err != nil {
		t.Fatalf("LoadSelfUpdate() error = %v", err)
	}
	if st.Phase != updates.SelfUpdateFailed {
		t.Fatalf("Phase = %q, want %q", st.Phase, updates.SelfUpdateFailed)
	}
	const wantMessage = "panel restarted on the old binary before the update finished; run the update again"
	if st.Message != wantMessage {
		t.Fatalf("Message = %q, want %q", st.Message, wantMessage)
	}
}

// TestFinalizeSelfUpdateStateBootLeavesTerminalPhaseAlone guards against
// finalizeSelfUpdateState clobbering an already-terminal phase (e.g. a
// clean, non-interrupted "completed" or "failed" outcome from a previous
// run) on every subsequent boot.
func TestFinalizeSelfUpdateStateBootLeavesTerminalPhaseAlone(t *testing.T) {
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "panvex.db"))
	if err != nil {
		t.Fatalf("sqlite.Open() error = %v", err)
	}
	defer store.Close()

	svc := updates.NewService(store)
	if err := svc.SaveSelfUpdate(context.Background(), updates.SelfUpdateState{
		Phase:       updates.SelfUpdateCompleted,
		FromVersion: "1.0.0",
		ToVersion:   "1.1.0",
	}); err != nil {
		t.Fatalf("SaveSelfUpdate() error = %v", err)
	}

	server := mustNew(t, Options{LoginTimingFloor: -1, Store: store, Version: "1.1.0"})
	defer server.Close()

	st, err := server.updatesSvc.LoadSelfUpdate(context.Background())
	if err != nil {
		t.Fatalf("LoadSelfUpdate() error = %v", err)
	}
	if st.Phase != updates.SelfUpdateCompleted {
		t.Fatalf("Phase = %q, want unchanged %q", st.Phase, updates.SelfUpdateCompleted)
	}
}

// TestHandlePanelUpdateRejectsConcurrentSelfUpdate covers Step 1(e): a
// second POST /settings/panel/update while a previous run's persisted phase
// is still non-terminal (downloading/installing/restart_pending) must be
// rejected with 409, protecting against a double download/install/restart
// that the pre-Task-2 handler had no defense against.
func TestHandlePanelUpdateRejectsConcurrentSelfUpdate(t *testing.T) {
	now := time.Date(2026, time.July, 15, 10, 0, 0, 0, time.UTC)
	server := testServerWithSQLite(t, now)
	server.version = "1.0.0"

	if _, _, err := server.auth.BootstrapUser(context.Background(), auth.BootstrapInput{
		Username: "admin",
		Password: "Admin1password",
		Role:     auth.RoleAdmin,
	}, now); err != nil {
		t.Fatalf("BootstrapUser() error = %v", err)
	}
	loginResp := performJSONRequest(t, server, http.MethodPost, "/api/auth/login", map[string]string{
		"username": "admin",
		"password": "Admin1password",
	}, nil)
	if loginResp.Code != http.StatusOK {
		t.Fatalf("POST /api/auth/login status = %d, want %d", loginResp.Code, http.StatusOK)
	}
	cookies := loginResp.Result().Cookies()

	if err := server.updatesSvc.SaveSelfUpdate(context.Background(), updates.SelfUpdateState{
		Phase:       updates.SelfUpdateDownloading,
		FromVersion: "1.0.0",
		ToVersion:   "1.1.0",
	}); err != nil {
		t.Fatalf("SaveSelfUpdate() error = %v", err)
	}

	resp := performJSONRequest(t, server, http.MethodPost, "/api/settings/panel/update", map[string]string{
		"target_version": "1.2.0",
	}, cookies)
	if resp.Code != http.StatusConflict {
		t.Fatalf("POST /api/settings/panel/update status = %d, want %d; body=%s", resp.Code, http.StatusConflict, resp.Body.String())
	}
}
