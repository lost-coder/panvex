package telemtupdate

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/agent/telemt"
)

// ---- test doubles ----

// fakeRunner scripts telemtrestart.CommandRunner.Run's return value per
// call: errs[0] for the first call, errs[1] for the second, and so on,
// repeating the last entry for any call beyond len(errs). A zero-value
// fakeRunner (nil errs) always succeeds, and any call is recorded so tests
// can assert it was never invoked.
type fakeRunner struct {
	mu   sync.Mutex
	errs []error
	// rejectCancelledCtx makes Run fail with ctx.Err() when called with an
	// already-cancelled/expired context, instead of ignoring ctx like a
	// typical test double. Used by TestExecute_ContextCancelledDuringPollRollsBack
	// to prove the rollback restart is issued on a context that survives
	// the poll loop's own ctx being cancelled (see Execute's use of
	// context.WithoutCancel for the rollback call).
	rejectCancelledCtx bool
	calls              int
}

func (f *fakeRunner) Run(ctx context.Context, _ string, _ ...string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.rejectCancelledCtx && ctx.Err() != nil {
		f.calls++
		return ctx.Err()
	}
	idx := f.calls
	f.calls++
	if len(f.errs) == 0 {
		return nil
	}
	if idx >= len(f.errs) {
		idx = len(f.errs) - 1
	}
	return f.errs[idx]
}

func (f *fakeRunner) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// fakeTelemtInfo is a scripted TelemtInfo: HealthReady always reports
// (ready, nil) and FetchSystemInfo always reports version — set both up
// front, since the state machine under test never needs them to change
// mid-poll (a timeout/ctx-cancel is what ends an unconfirmed poll, not the
// fake changing its answer).
type fakeTelemtInfo struct {
	mu          sync.Mutex
	ready       bool
	version     string
	healthCalls int
}

func (f *fakeTelemtInfo) HealthReady(_ context.Context) (bool, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.healthCalls++
	return f.ready, "", nil
}

func (f *fakeTelemtInfo) FetchSystemInfo(_ context.Context) (telemt.SystemInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return telemt.SystemInfo{Version: f.version}, nil
}

func (f *fakeTelemtInfo) HealthCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.healthCalls
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func fastTuning() Tuning {
	return Tuning{HealthTimeout: 100 * time.Millisecond, HealthPollInterval: 10 * time.Millisecond}
}

// buildArchive tar.gz's a single regular member named "telemt" holding
// content, and returns the archive bytes plus its sha256 hex digest — the
// same shape fetchToTemp/extractBinary expect (see download_test.go's
// makeTarGz for the sibling helper used by Task 3/4's tests).
func buildArchive(t *testing.T, content []byte) (archiveBytes []byte, sha256hex string) {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{
		Name:     "telemt",
		Mode:     0o755,
		Size:     int64(len(content)),
		Typeflag: tar.TypeReg,
	}); err != nil {
		t.Fatalf("write tar header: %v", err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatalf("write tar body: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar writer: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("close gzip writer: %v", err)
	}
	sum := sha256.Sum256(buf.Bytes())
	return buf.Bytes(), hex.EncodeToString(sum[:])
}

// updateFixture bundles everything TestExecute_* needs: a real Telemt
// "binary" file on disk, and an httptest.TLS server serving a valid release
// archive + matching checksum sidecar at the exact asset path Execute will
// compute via ResolveAsset. currentVersion/target Version are fixed at
// "1.2.3"/"1.2.4" (a real upgrade, not the equal-version no-op path — that
// gets its own minimal test since it must never reach this fixture's
// server at all).
type updateFixture struct {
	dir        string
	binaryPath string
	payload    Payload
	cfg        Config
}

func newUpdateFixture(t *testing.T, newContent []byte) *updateFixture {
	t.Helper()
	dir := t.TempDir()
	binaryPath := filepath.Join(dir, "telemt")
	if err := os.WriteFile(binaryPath, []byte("old-telemt-binary"), 0o755); err != nil { //nolint:gosec // G306: test fixture, executable bit matches production binaries
		t.Fatalf("seed binaryPath: %v", err)
	}

	assetName, err := ResolveAsset("")
	if err != nil {
		t.Fatalf("ResolveAsset: %v", err)
	}
	archiveBytes, sum := buildArchive(t, newContent)

	mux := http.NewServeMux()
	mux.HandleFunc("/"+assetName+".tar.gz", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(archiveBytes)
	})
	mux.HandleFunc("/"+assetName+".tar.gz.sha256", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(sum))
	})
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)

	return &updateFixture{
		dir:        dir,
		binaryPath: binaryPath,
		payload: Payload{
			Version:        "1.2.4",
			ReleaseBaseURL: srv.URL,
			RestartSpec:    "command:telemt-restart",
			BinaryPath:     binaryPath,
		},
		cfg: Config{
			HTTPClient:   srv.Client(),
			AllowedHosts: []string{hostOf(t, srv.URL)},
			MaxArchive:   1 << 20,
		},
	}
}

const currentVersion = "1.2.3"

// ---- (a) happy path ----

func TestExecute_HappyPathUpdatesAndConfirmsHealth(t *testing.T) {
	fx := newUpdateFixture(t, []byte("new-telemt-binary-content"))
	runner := &fakeRunner{}
	info := &fakeTelemtInfo{ready: true, version: fx.payload.Version}

	outcome, err := executeWith(context.Background(), fx.payload, currentVersion, info, runner, testLogger(), fx.cfg, fastTuning())
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !outcome.Updated {
		t.Fatalf("Updated = false, want true")
	}
	if outcome.KeepBackup {
		t.Fatalf("KeepBackup = true, want false")
	}
	if runner.callCount() != 1 {
		t.Fatalf("runner called %d times, want 1", runner.callCount())
	}

	got, err := os.ReadFile(fx.binaryPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new-telemt-binary-content" {
		t.Fatalf("binaryPath content = %q, want new content", got)
	}
	if _, statErr := os.Stat(fx.binaryPath + backupSuffix); !os.IsNotExist(statErr) {
		t.Fatalf(".bak should have been removed after a confirmed healthy update, stat err = %v", statErr)
	}
}

// ---- (b) restart command fails ----

func TestExecute_RestartFailsRestoresBinaryWithoutRetrying(t *testing.T) {
	fx := newUpdateFixture(t, []byte("new-telemt-binary-content"))
	runner := &fakeRunner{errs: []error{errors.New("systemctl restart: unit not found")}}
	info := &fakeTelemtInfo{ready: true, version: fx.payload.Version}

	outcome, err := executeWith(context.Background(), fx.payload, currentVersion, info, runner, testLogger(), fx.cfg, fastTuning())
	if err == nil {
		t.Fatal("expected non-nil error when restart fails")
	}
	if outcome.Updated {
		t.Fatalf("Updated = true, want false")
	}
	if outcome.KeepBackup {
		t.Fatalf("KeepBackup = true, want false (restore succeeded)")
	}
	if !strings.Contains(outcome.Message, "restart command failed") || !strings.Contains(outcome.Message, "binary restored") {
		t.Fatalf("Message = %q, missing expected phrases", outcome.Message)
	}
	if runner.callCount() != 1 {
		t.Fatalf("runner called %d times, want 1 (no retry on a bare restart failure)", runner.callCount())
	}
	if info.HealthCalls() != 0 {
		t.Fatalf("HealthReady called %d times, want 0 (never reached the health-gate)", info.HealthCalls())
	}

	got, err := os.ReadFile(fx.binaryPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "old-telemt-binary" {
		t.Fatalf("binaryPath content = %q, want the restored old binary", got)
	}
}

// ---- (c) health never confirms the new version ----

func TestExecute_HealthNeverConfirmsRollsBackWithSecondRestart(t *testing.T) {
	fx := newUpdateFixture(t, []byte("new-telemt-binary-content"))
	runner := &fakeRunner{} // both calls succeed
	// ready stays false the whole poll: health never confirms.
	info := &fakeTelemtInfo{ready: false, version: fx.payload.Version}

	outcome, err := executeWith(context.Background(), fx.payload, currentVersion, info, runner, testLogger(), fx.cfg, fastTuning())
	if err == nil {
		t.Fatal("expected non-nil error when health never confirms")
	}
	if outcome.Updated {
		t.Fatalf("Updated = true, want false")
	}
	if outcome.KeepBackup {
		t.Fatalf("KeepBackup = true, want false (rollback restart succeeded)")
	}
	wantMsg := "update failed health check; rolled back to " + currentVersion
	if outcome.Message != wantMsg {
		t.Fatalf("Message = %q, want %q", outcome.Message, wantMsg)
	}
	if runner.callCount() != 2 {
		t.Fatalf("runner called %d times, want 2 (post-swap restart + rollback restart)", runner.callCount())
	}

	got, err := os.ReadFile(fx.binaryPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "old-telemt-binary" {
		t.Fatalf("binaryPath content = %q, want the restored old binary", got)
	}
}

// ---- (d) health fails AND the rollback recovery also fails ----
//
// Split into the two distinct sub-cases the owner's approved semantics
// distinguish: restoreBackup succeeding but the retried Restart failing
// (KeepBackup=false — the .bak was already consumed by the successful
// restore, so there is genuinely nothing left to "keep"), versus
// restoreBackup itself failing (KeepBackup=true — a failed rename leaves
// its source untouched, so the .bak is genuinely still on disk).

func TestExecute_HealthFailsRestoreOKButRollbackRestartFailsNoKeepBackup(t *testing.T) {
	fx := newUpdateFixture(t, []byte("new-telemt-binary-content"))
	rollbackErr := errors.New("systemctl restart: connection refused")
	// Call 1 (post-swap restart) succeeds; call 2 (rollback restart) fails.
	runner := &fakeRunner{errs: []error{nil, rollbackErr}}
	info := &fakeTelemtInfo{ready: false, version: fx.payload.Version}

	outcome, err := executeWith(context.Background(), fx.payload, currentVersion, info, runner, testLogger(), fx.cfg, fastTuning())
	if err == nil {
		t.Fatal("expected non-nil error on double failure")
	}
	if outcome.Updated {
		t.Fatalf("Updated = true, want false")
	}
	if outcome.KeepBackup {
		t.Fatalf("KeepBackup = true, want false (restore already succeeded; the .bak is already consumed, nothing left to keep)")
	}
	wantSubstrs := []string{
		"old binary restored to " + fx.binaryPath,
		"was not restarted",
		"rollback restart failed",
		"manually restart the service",
	}
	for _, s := range wantSubstrs {
		if !strings.Contains(outcome.Message, s) {
			t.Fatalf("Message = %q, missing %q", outcome.Message, s)
		}
	}
	if runner.callCount() != 2 {
		t.Fatalf("runner called %d times, want 2", runner.callCount())
	}

	got, err := os.ReadFile(fx.binaryPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "old-telemt-binary" {
		t.Fatalf("binaryPath content = %q, want the restored old binary", got)
	}
	if _, statErr := os.Stat(fx.binaryPath + backupSuffix); !os.IsNotExist(statErr) {
		t.Fatalf(".bak should be gone (consumed by the successful restoreBackup), stat err = %v", statErr)
	}
}

// sabotagingTelemtInfo always reports not-ready (so the health-gate always
// times out), and on its first call replaces backupPath — the .bak
// swapBinary left next to the binary — with a directory. That makes the
// later os.Rename(backupPath, binaryPath) inside restoreBackup fail with
// ENOTDIR (renaming a directory onto an existing regular file is refused),
// deterministically and without relying on permission bits (which running
// as root would otherwise bypass) — and, crucially, a failed rename leaves
// its source (this directory) untouched, so backupPath genuinely still
// exists on disk afterward.
type sabotagingTelemtInfo struct {
	mu         sync.Mutex
	backupPath string
	sabotaged  bool
}

func (f *sabotagingTelemtInfo) HealthReady(_ context.Context) (bool, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.sabotaged {
		f.sabotaged = true
		_ = os.Remove(f.backupPath)
		_ = os.Mkdir(f.backupPath, 0o755) //nolint:gosec // G301: test fixture, permissions irrelevant
	}
	return false, "", nil
}

func (f *sabotagingTelemtInfo) FetchSystemInfo(_ context.Context) (telemt.SystemInfo, error) {
	return telemt.SystemInfo{}, nil
}

func TestExecute_HealthFailsAndRestoreBackupItselfFailsKeepsBackup(t *testing.T) {
	fx := newUpdateFixture(t, []byte("new-telemt-binary-content"))
	runner := &fakeRunner{}
	backupPath := fx.binaryPath + backupSuffix
	info := &sabotagingTelemtInfo{backupPath: backupPath}

	outcome, err := executeWith(context.Background(), fx.payload, currentVersion, info, runner, testLogger(), fx.cfg, fastTuning())
	if err == nil {
		t.Fatal("expected non-nil error when restoreBackup itself fails")
	}
	if outcome.Updated {
		t.Fatalf("Updated = true, want false")
	}
	if !outcome.KeepBackup {
		t.Fatalf("KeepBackup = false, want true (restoreBackup failed, backup genuinely still on disk)")
	}
	wantSubstrs := []string{
		"manual recovery required",
		"backup kept at " + backupPath,
	}
	for _, s := range wantSubstrs {
		if !strings.Contains(outcome.Message, s) {
			t.Fatalf("Message = %q, missing %q", outcome.Message, s)
		}
	}
	// restoreBackup failing must not trigger a rollback Restart attempt:
	// there is nothing safe to restart into (the binary in place is still
	// the unhealthy new one).
	if runner.callCount() != 1 {
		t.Fatalf("runner called %d times, want 1 (post-swap restart only, no rollback attempt)", runner.callCount())
	}

	if _, statErr := os.Stat(backupPath); statErr != nil {
		t.Fatalf("backup should still exist on disk, stat err = %v", statErr)
	}
}

// ---- (e) equal version: no-op, no download ----

func TestExecute_EqualVersionIsNoOpWithoutDownloading(t *testing.T) {
	var hits int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	p := Payload{
		Version:        currentVersion,
		ReleaseBaseURL: srv.URL,
		RestartSpec:    "command:telemt-restart",
		BinaryPath:     filepath.Join(t.TempDir(), "telemt"), // never created; must never be touched
	}
	cfg := Config{HTTPClient: srv.Client(), AllowedHosts: []string{hostOf(t, srv.URL)}, MaxArchive: 1 << 20}
	runner := &fakeRunner{}
	info := &fakeTelemtInfo{}

	outcome, err := executeWith(context.Background(), p, currentVersion, info, runner, testLogger(), cfg, fastTuning())
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if outcome.Updated {
		t.Fatalf("Updated = true, want false")
	}
	if !strings.Contains(outcome.Message, "already at target version") {
		t.Fatalf("Message = %q, missing expected phrase", outcome.Message)
	}
	if runner.callCount() != 0 {
		t.Fatalf("runner called %d times, want 0", runner.callCount())
	}
	if info.HealthCalls() != 0 {
		t.Fatalf("HealthReady called %d times, want 0", info.HealthCalls())
	}
	if atomic.LoadInt32(&hits) != 0 {
		t.Fatalf("release server was hit %d times, want 0 (downgrade gate must short-circuit before any download)", hits)
	}
}

// ---- (f) context cancelled mid-poll: rolls back like (c) ----

func TestExecute_ContextCancelledDuringPollRollsBack(t *testing.T) {
	fx := newUpdateFixture(t, []byte("new-telemt-binary-content"))
	// rejectCancelledCtx proves the rollback restart runs on a context that
	// survives the outer ctx's cancellation (Execute must strip
	// cancellation via context.WithoutCancel before retrying the restart —
	// otherwise this fake would see ctx.Err() != nil and fail the call).
	runner := &fakeRunner{rejectCancelledCtx: true}
	info := &fakeTelemtInfo{ready: false, version: fx.payload.Version}

	// Tuning's health timeout is generous; the outer ctx's much shorter
	// deadline is what actually ends the poll.
	tuning := Tuning{HealthTimeout: time.Second, HealthPollInterval: 10 * time.Millisecond}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	outcome, err := executeWith(ctx, fx.payload, currentVersion, info, runner, testLogger(), fx.cfg, tuning)
	if err == nil {
		t.Fatal("expected non-nil error when ctx is cancelled mid-poll")
	}
	if outcome.Updated {
		t.Fatalf("Updated = true, want false")
	}
	if outcome.KeepBackup {
		t.Fatalf("KeepBackup = true, want false (rollback restart succeeded on the surviving context)")
	}
	wantMsg := "update failed health check; rolled back to " + currentVersion
	if outcome.Message != wantMsg {
		t.Fatalf("Message = %q, want %q", outcome.Message, wantMsg)
	}
	if runner.callCount() != 2 {
		t.Fatalf("runner called %d times, want 2", runner.callCount())
	}

	got, err := os.ReadFile(fx.binaryPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "old-telemt-binary" {
		t.Fatalf("binaryPath content = %q, want the restored old binary", got)
	}
}

// ---- bonus: malformed checksum sidecar fails closed ----
//
// Not one of the brief's lettered scenarios, but isHex64's sidecar-shape
// validation is new logic introduced by Execute (Task 3's fetchChecksum
// only checks "non-empty", not "looks like a digest"), so it earns its own
// direct test rather than relying on incidental coverage from (a)-(f).

func TestExecute_MalformedChecksumSidecarFailsClosedWithoutSwapping(t *testing.T) {
	dir := t.TempDir()
	binaryPath := filepath.Join(dir, "telemt")
	if err := os.WriteFile(binaryPath, []byte("old-telemt-binary"), 0o755); err != nil { //nolint:gosec // G306: test fixture
		t.Fatal(err)
	}

	assetName, err := ResolveAsset("")
	if err != nil {
		t.Fatalf("ResolveAsset: %v", err)
	}
	archiveBytes, _ := buildArchive(t, []byte("new-telemt-binary-content"))

	mux := http.NewServeMux()
	mux.HandleFunc("/"+assetName+".tar.gz", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(archiveBytes)
	})
	mux.HandleFunc("/"+assetName+".tar.gz.sha256", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("not-a-valid-digest"))
	})
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)

	p := Payload{
		Version:        "1.2.4",
		ReleaseBaseURL: srv.URL,
		RestartSpec:    "command:telemt-restart",
		BinaryPath:     binaryPath,
	}
	cfg := Config{HTTPClient: srv.Client(), AllowedHosts: []string{hostOf(t, srv.URL)}, MaxArchive: 1 << 20}
	runner := &fakeRunner{}
	info := &fakeTelemtInfo{}

	_, err = executeWith(context.Background(), p, currentVersion, info, runner, testLogger(), cfg, fastTuning())
	if err == nil || !strings.Contains(err.Error(), "malformed checksum sidecar") {
		t.Fatalf("err = %v, want it to mention a malformed checksum sidecar", err)
	}
	if runner.callCount() != 0 {
		t.Fatalf("runner called %d times, want 0", runner.callCount())
	}

	got, err := os.ReadFile(binaryPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "old-telemt-binary" {
		t.Fatalf("binaryPath was modified: %q", got)
	}
	if _, statErr := os.Stat(binaryPath + backupSuffix); !os.IsNotExist(statErr) {
		t.Fatalf("no backup should exist, stat err = %v", statErr)
	}
}
