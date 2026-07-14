package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/controlplane/auth"
	"github.com/lost-coder/panvex/internal/controlplane/csrf"
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

// bootstrapAdminAndLogin creates an admin user and logs in, returning the
// session cookies needed for an authenticated HTTP round trip (mirrors the
// login sequence inlined in TestHandlePanelUpdateRejectsConcurrentSelfUpdate
// above, factored out because the two tests below need it too).
func bootstrapAdminAndLogin(t *testing.T, server *Server, now time.Time) []*http.Cookie {
	t.Helper()
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
	return loginResp.Result().Cookies()
}

// newPanelUpdateRequest builds a real, CSRF-satisfying POST
// /api/settings/panel/update request — the same header/cookie wiring
// performJSONRequestWithHeaders uses (http_test.go) — but returns it
// unexecuted so a caller can drive it through its own http.ResponseWriter.
// That is needed here: the ordering test below wraps the recorder to snapshot
// state mid-response, and the concurrency test needs two independent request
// objects running on separate goroutines (a shared *http.Request cannot be
// replayed concurrently).
func newPanelUpdateRequest(t *testing.T, server *Server, body any, cookies []*http.Cookie) *http.Request {
	t.Helper()
	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/settings/panel/update", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	for _, c := range cookies {
		req.AddCookie(c)
	}
	req.Header.Set("Origin", "http://"+req.Host)
	for _, c := range cookies {
		if (c.Name == sessionCookieName || c.Name == sessionCookieNameHostPrefix) && c.Value != "" {
			req.Header.Set(csrf.TokenHeader, server.csrfManager.TokenForSession(c.Value))
			break
		}
	}
	return req
}

// stubSelfUpdateSeams wires deterministic, network-free/binary-free fakes for
// performPanelUpdate's three seams so a background run triggered by a real
// 202 response completes quickly and harmlessly instead of hitting the
// network or replacing the test binary. runs counts how many times the
// installer actually executed, i.e. how many performPanelUpdate runs reached
// the install step.
func stubSelfUpdateSeams(t *testing.T, server *Server, runs *int32) {
	t.Helper()
	server.selfUpdateChecksumFetcher = func(ctx context.Context, checksumURL, token string) (string, bool) {
		return "deadbeef", true
	}
	server.selfUpdateArchiveDownloader = func(ctx context.Context, downloadURL, expectedChecksum, token string) (string, bool) {
		return filepath.Join(t.TempDir(), "fake-archive.tar.gz"), true
	}
	server.selfUpdateInstaller = func(ctx context.Context, archivePath string) bool {
		if runs != nil {
			atomic.AddInt32(runs, 1)
		}
		return true
	}
}

// TestHandlePanelUpdateConcurrentRequestsClaimExactlyOnce is the regression
// test for the R11b Task 2 review's Critical finding: rejectConcurrentSelfUpdate
// (a Load + phase check) and the "downloading" phase persist used to be two
// independent, unlocked operations — a TOCTOU window a second near-
// simultaneous POST /settings/panel/update (double-click, two admin tabs, a
// retrying client) could slip through, both spawning performPanelUpdate, each
// independently racing updates.AtomicReplaceBinary
// (internal/controlplane/updates/download.go:254) against the other.
//
// This proves the fix: of two concurrent requests, exactly one is accepted
// (202) and claims the run, the other is rejected (409), and exactly one
// background performPanelUpdate run actually reaches the installer.
//
// Determinism (no sleep-based flakiness): server.selfUpdateClaimHook is a
// test-only seam invoked inside claimSelfUpdate's locked section, after the
// phase check passes and before the phase is persisted. The first goroutine
// to reach it signals `started` and blocks on `proceed` — under the fix, it
// is still holding selfUpdateClaimMu at that point. The second goroutine is
// only launched after `started` fires, guaranteeing it attempts its own claim
// while the first is deliberately parked: under the fix it must block on the
// mutex (so its own Load only happens, and observes "downloading", once it
// finally acquires the lock after the first request releases it); without
// the fix (no lock) it sails through the same unlocked check the first
// request already passed, and `secondArrived` closes while the first request
// is still parked — the observable signature of the bug.
func TestHandlePanelUpdateConcurrentRequestsClaimExactlyOnce(t *testing.T) {
	now := time.Date(2026, time.July, 15, 10, 0, 0, 0, time.UTC)
	server := testServerWithSQLite(t, now)
	server.version = "1.0.0"
	server.updateState = UpdateState{
		LatestPanelVersion: "1.1.0",
		PanelDownloadURL:   "https://example.invalid/download",
		PanelChecksumURL:   "https://example.invalid/checksum",
	}

	var runs int32
	stubSelfUpdateSeams(t, server, &runs)

	cookies := bootstrapAdminAndLogin(t, server, now)

	var hookCalls int32
	started := make(chan struct{})
	proceed := make(chan struct{})
	secondArrived := make(chan struct{})
	server.selfUpdateClaimHook = func() {
		switch atomic.AddInt32(&hookCalls, 1) {
		case 1:
			close(started)
			<-proceed
		case 2:
			close(secondArrived)
		}
	}

	reqA := newPanelUpdateRequest(t, server, map[string]string{"target_version": "1.1.0"}, cookies)
	reqB := newPanelUpdateRequest(t, server, map[string]string{"target_version": "1.1.0"}, cookies)
	recA := httptest.NewRecorder()
	recB := httptest.NewRecorder()

	doneA := make(chan struct{})
	doneB := make(chan struct{})

	go func() {
		defer close(doneA)
		server.Handler().ServeHTTP(recA, reqA)
	}()
	<-started // request A is parked inside claimSelfUpdate's locked section

	go func() {
		defer close(doneB)
		server.Handler().ServeHTTP(recB, reqB)
	}()

	// Give request B a bounded window to either slip through (bug) or block
	// on the mutex (fix) before releasing A. This is a ceiling on how long we
	// wait for an observation, not the mechanism that creates the
	// concurrency itself (that's `started`/`proceed`), so it does not make
	// the test's pass/fail outcome timing-dependent.
	select {
	case <-secondArrived:
	case <-time.After(2 * time.Second):
	}
	close(proceed)

	<-doneA
	<-doneB
	server.bgWG.Wait() // let any spawned performPanelUpdate goroutine finish

	codes := []int{recA.Code, recB.Code}
	var accepted, conflict int
	for _, c := range codes {
		switch c {
		case http.StatusAccepted:
			accepted++
		case http.StatusConflict:
			conflict++
		}
	}
	if accepted != 1 || conflict != 1 {
		t.Fatalf("status codes = %v (bodies: A=%s B=%s), want exactly one %d and one %d",
			codes, recA.Body.String(), recB.Body.String(), http.StatusAccepted, http.StatusConflict)
	}
	if got := atomic.LoadInt32(&runs); got != 1 {
		t.Fatalf("performPanelUpdate reached the installer %d times, want exactly 1", got)
	}
}

// phaseSnapshotWriter wraps an http.ResponseWriter to capture the persisted
// self-update phase at the exact moment the handler calls WriteHeader. This
// is the only way to prove setSelfUpdatePhase(downloading) executes BEFORE
// writeJSON writes the response rather than after: both statements run
// synchronously in the same handler invocation, so asserting on the phase
// only after ServeHTTP returns would pass under either ordering. Sampling at
// WriteHeader time is what actually distinguishes them, and is what would
// catch a regression that reordered the two statements.
type phaseSnapshotWriter struct {
	http.ResponseWriter
	svc          *updates.Service
	captured     bool
	phaseAtWrite updates.SelfUpdatePhase
}

func (w *phaseSnapshotWriter) WriteHeader(status int) {
	if !w.captured {
		w.captured = true
		if st, err := w.svc.LoadSelfUpdate(context.Background()); err == nil {
			w.phaseAtWrite = st.Phase
		}
	}
	w.ResponseWriter.WriteHeader(status)
}

// TestHandlePanelUpdatePersistsDownloadingPhaseBeforeResponse covers the
// Important finding: nothing proved the "downloading" phase lands in the
// store before the 202 is written, which is the entire point of doing it
// synchronously in the handler instead of inside performPanelUpdate's
// goroutine (so the dashboard's first post-request refetch already observes
// it). A reordering of claimSelfUpdate/writeJSON in handlePanelUpdate would
// not be caught by any pre-existing test, only by sampling the persisted
// phase at the instant WriteHeader is called.
func TestHandlePanelUpdatePersistsDownloadingPhaseBeforeResponse(t *testing.T) {
	now := time.Date(2026, time.July, 15, 10, 0, 0, 0, time.UTC)
	server := testServerWithSQLite(t, now)
	server.version = "1.0.0"
	server.updateState = UpdateState{
		LatestPanelVersion: "1.1.0",
		PanelDownloadURL:   "https://example.invalid/download",
		PanelChecksumURL:   "https://example.invalid/checksum",
	}
	stubSelfUpdateSeams(t, server, nil)

	cookies := bootstrapAdminAndLogin(t, server, now)

	req := newPanelUpdateRequest(t, server, map[string]string{"target_version": "1.1.0"}, cookies)
	rec := httptest.NewRecorder()
	wrapped := &phaseSnapshotWriter{ResponseWriter: rec, svc: server.updatesSvc}

	server.Handler().ServeHTTP(wrapped, req)
	server.bgWG.Wait()

	if rec.Code != http.StatusAccepted {
		t.Fatalf("POST /api/settings/panel/update status = %d, want %d; body=%s", rec.Code, http.StatusAccepted, rec.Body.String())
	}
	if wrapped.phaseAtWrite != updates.SelfUpdateDownloading {
		t.Fatalf("persisted phase at WriteHeader time = %q, want %q — the downloading phase must be persisted BEFORE the response is written",
			wrapped.phaseAtWrite, updates.SelfUpdateDownloading)
	}
}
