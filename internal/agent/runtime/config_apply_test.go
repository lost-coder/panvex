package runtime

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lost-coder/panvex/internal/agent/telemt"
	"github.com/lost-coder/panvex/internal/gatewayrpc"
)

func TestBackupAndRestoreConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("original = true\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	backup, err := backupConfigFile(path)
	if err != nil {
		t.Fatalf("backupConfigFile: %v", err)
	}
	if backup == "" {
		t.Fatal("expected non-empty backup path")
	}

	// Simulate a bad write.
	if err := os.WriteFile(path, []byte("broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := restoreConfigFile(backup, path); err != nil {
		t.Fatalf("restoreConfigFile: %v", err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "original = true\n" {
		t.Fatalf("restore mismatch: %q", got)
	}
}

type fakeTelemt struct {
	patchResult telemt.PatchConfigResult
	patchErr    error
	healthSeq   []bool // successive HealthReady results; empty => always true
	healthErr   error
	patchedWith map[string]any
	patchedRev  string // expectedRevision observed on the last PatchConfig call
	patchCalls  int

	managedConfig    map[string]any
	managedRevision  string
	managedConfigErr error
	getManagedCalls  int

	// onPatch, if set, runs during PatchConfig before the fake returns its
	// canned result. Used to inject a REAL restore failure at exactly the
	// point production code would hit it (after the backup is taken, before
	// the ensuing restore/rollback), e.g. by chmod'ing the config's directory
	// read-only so atomicfile.Write's os.CreateTemp fails for real.
	onPatch func()

	submitErr      error
	submitAccepted telemt.ReloadAccepted
	// submitFn, if set, is called instead of returning submitAccepted/submitErr
	// — gives tests per-call control (e.g. fail once, then succeed).
	submitFn    func() (telemt.ReloadAccepted, error)
	statusSeq   []telemt.ReloadStatus // successive GetReloadStatus results
	submitCalls int
	statusCalls int
}

func (f *fakeTelemt) SubmitReload(_ context.Context, mode string, timeoutSecs int, failurePolicy, ifMatch string) (telemt.ReloadAccepted, error) {
	f.submitCalls++
	if f.submitFn != nil {
		return f.submitFn()
	}
	if f.submitErr != nil {
		return telemt.ReloadAccepted{}, f.submitErr
	}
	return f.submitAccepted, nil
}

func (f *fakeTelemt) GetReloadStatus(_ context.Context, _ uint64) (telemt.ReloadStatus, error) {
	i := f.statusCalls
	f.statusCalls++
	if i >= len(f.statusSeq) {
		i = len(f.statusSeq) - 1
	}
	return f.statusSeq[i], nil
}

func (f *fakeTelemt) PatchConfig(_ context.Context, patch map[string]any, expectedRevision string) (telemt.PatchConfigResult, error) {
	f.patchCalls++
	f.patchedWith = patch
	f.patchedRev = expectedRevision
	if f.onPatch != nil {
		f.onPatch()
	}
	return f.patchResult, f.patchErr
}

func (f *fakeTelemt) GetManagedConfig(context.Context) (map[string]any, string, error) {
	f.getManagedCalls++
	if f.managedConfigErr != nil {
		return nil, "", f.managedConfigErr
	}
	return f.managedConfig, f.managedRevision, nil
}
func (f *fakeTelemt) HealthReady(context.Context) (bool, string, error) {
	if f.healthErr != nil {
		return false, "", f.healthErr
	}
	if len(f.healthSeq) == 0 {
		return true, "", nil
	}
	v := f.healthSeq[0]
	f.healthSeq = f.healthSeq[1:]
	return v, "", nil
}

type fakeRestarter struct {
	restartErr     error
	restartErrSeq  []error // successive Restart results; consumed before restartErr
	restarts       int
	restartCtxErrs []error // ctx.Err() observed at each Restart call
}

func (f *fakeRestarter) Restart(ctx context.Context) error {
	f.restarts++
	f.restartCtxErrs = append(f.restartCtxErrs, ctx.Err())
	if len(f.restartErrSeq) > 0 {
		err := f.restartErrSeq[0]
		f.restartErrSeq = f.restartErrSeq[1:]
		return err
	}
	return f.restartErr
}

func writeTempConfig(t *testing.T) string {
	t.Helper()
	p := t.TempDir() + "/config.toml"
	if err := os.WriteFile(p, []byte("tls_domain=\"orig\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestConfigApplyHotChangeNoRestart(t *testing.T) {
	path := writeTempConfig(t)
	tc := &fakeTelemt{patchResult: telemt.PatchConfigResult{Revision: "r2", RestartRequired: false, Changed: []string{"general"}}}
	res := runConfigApply(context.Background(), configApplyDeps{telemt: tc, configPath: path},
		configApplyPayload{Patch: map[string]any{"general": map[string]any{"log_level": "debug"}}})
	if !res.success {
		t.Fatalf("expected success, got %q", res.message)
	}
}

func TestConfigApplyPreflightUnhealthyAborts(t *testing.T) {
	path := writeTempConfig(t)
	tc := &fakeTelemt{healthSeq: []bool{false}} // preflight sees unhealthy
	res := runConfigApply(context.Background(), configApplyDeps{telemt: tc, configPath: path},
		configApplyPayload{Patch: map[string]any{"censorship": map[string]any{"tls_domain": "b"}}})
	if res.success {
		t.Fatalf("expected failure when Telemt is unhealthy at preflight")
	}
	if tc.patchedWith != nil {
		t.Fatalf("patch must NOT be attempted when preflight fails")
	}
}

func TestConfigApplyRevisionConflictNoRestart(t *testing.T) {
	path := writeTempConfig(t)
	tc := &fakeTelemt{patchErr: telemt.ErrConfigRevisionConflict, healthSeq: []bool{true}}
	res := runConfigApply(context.Background(), configApplyDeps{telemt: tc, configPath: path},
		configApplyPayload{ExpectedRevision: "stale", Patch: map[string]any{"censorship": map[string]any{"tls_domain": "b"}}})
	if res.success {
		t.Fatalf("expected failure on revision conflict")
	}
	if !strings.Contains(res.message, "revision conflict") {
		t.Fatalf("message = %q, want it to mention revision conflict", res.message)
	}
	if !strings.Contains(res.message, "re-fetch and retry") {
		t.Fatalf("message = %q, want it to instruct re-fetch and retry (not a silent success/blind overwrite)", res.message)
	}
	// The caller-supplied expected_revision must be forwarded verbatim; the
	// agent must not overwrite it with a freshly fetched one when present.
	if tc.getManagedCalls != 0 {
		t.Fatalf("expected_revision was supplied by the caller, agent must not fetch it: getManagedCalls=%d", tc.getManagedCalls)
	}
	if tc.patchedRev != "stale" {
		t.Fatalf("patchedRev = %q, want caller-supplied %q forwarded unchanged", tc.patchedRev, "stale")
	}
}

// TestConfigApplyEmptyRevisionFetchesCurrentForCAS guards the D3 fix: a
// config.apply job with an empty expected_revision (the shape every existing
// caller — control-plane enqueue, UI — sends today) must NOT result in a
// blind PATCH with no If-Match. The agent fetches Telemt's current revision
// via GetManagedConfig and forwards THAT as the CAS token, so a
// retried/duplicated apply still goes through Telemt's 409 guard instead of
// bypassing it.
func TestConfigApplyEmptyRevisionFetchesCurrentForCAS(t *testing.T) {
	path := writeTempConfig(t)
	tc := &fakeTelemt{
		patchResult:     telemt.PatchConfigResult{Revision: "r2", RestartRequired: false},
		managedRevision: "r1",
		healthSeq:       []bool{true, true},
	}
	res := runConfigApply(context.Background(), configApplyDeps{telemt: tc, configPath: path},
		configApplyPayload{Patch: map[string]any{"general": map[string]any{"log_level": "debug"}}})

	if !res.success {
		t.Fatalf("expected success, got %q", res.message)
	}
	if tc.getManagedCalls != 1 {
		t.Fatalf("expected exactly 1 GetManagedConfig call to resolve the CAS token, got %d", tc.getManagedCalls)
	}
	if tc.patchedRev != "r1" {
		t.Fatalf("PatchConfig If-Match/expectedRevision = %q, want agent-fetched revision %q (blind write bug: empty)", tc.patchedRev, "r1")
	}
}

// TestConfigApplyEmptyRevisionDuplicateIsConflictNotDoubleApply guards the
// core idempotency property end to end: simulate a duplicated/retried
// config.apply delivery where Telemt's revision has already moved (someone
// else applied first, or the previous delivery of the same job already
// succeeded). The agent-resolved CAS token no longer matches what Telemt
// holds by the time PATCH lands, so Telemt returns 409 and the result must
// be a conflict, not a second blind re-apply.
func TestConfigApplyEmptyRevisionDuplicateIsConflictNotDoubleApply(t *testing.T) {
	path := writeTempConfig(t)
	tc := &fakeTelemt{
		managedRevision: "r1",                             // agent observes r1 when it fetches for CAS...
		patchErr:        telemt.ErrConfigRevisionConflict, // ...but Telemt has already moved past it.
		healthSeq:       []bool{true},
	}
	res := runConfigApply(context.Background(), configApplyDeps{telemt: tc, configPath: path},
		configApplyPayload{Patch: map[string]any{"general": map[string]any{"log_level": "debug"}}})

	if res.success {
		t.Fatalf("expected conflict/no-op, got success (blind re-apply): %q", res.message)
	}
	if tc.patchCalls != 1 {
		t.Fatalf("expected exactly 1 PatchConfig attempt (no blind retry/double-apply), got %d", tc.patchCalls)
	}
	if !strings.Contains(res.message, "revision conflict") {
		t.Fatalf("message = %q, want it to surface the revision conflict clearly", res.message)
	}
}

// TestConfigApplyEmptyRevisionFetchFailureAborts guards the failure path of
// the agent-fetch: if GetManagedConfig itself errors, the apply must abort
// with a clear message rather than falling through to a blind PATCH with an
// empty If-Match.
func TestConfigApplyEmptyRevisionFetchFailureAborts(t *testing.T) {
	path := writeTempConfig(t)
	tc := &fakeTelemt{managedConfigErr: errors.New("telemt unreachable"), healthSeq: []bool{true}}
	res := runConfigApply(context.Background(), configApplyDeps{telemt: tc, configPath: path},
		configApplyPayload{Patch: map[string]any{"general": map[string]any{"log_level": "debug"}}})

	if res.success {
		t.Fatalf("expected failure when the agent cannot resolve a CAS revision")
	}
	if tc.patchCalls != 0 {
		t.Fatalf("PatchConfig must not be attempted when the revision fetch fails, got %d calls", tc.patchCalls)
	}
}

func TestConfigApplyEmptyPatchFails(t *testing.T) {
	path := writeTempConfig(t)
	res := runConfigApply(context.Background(), configApplyDeps{telemt: &fakeTelemt{}, configPath: path},
		configApplyPayload{Patch: nil})
	if res.success {
		t.Fatalf("expected failure on empty patch")
	}
}

func TestHandleConfigApplyJobHotChange(t *testing.T) {
	path := writeTempConfig(t)
	a := New(Config{TelemtConfigPath: path}, &fakeTelemtClient{})
	job := &gatewayrpc.JobCommand{Action: "config.apply", PayloadJson: `{"patch":{"general":{"log_level":"debug"}}}`}
	res := a.handleConfigApplyJob(context.Background(), job, &gatewayrpc.JobResult{})
	if !res.Success {
		t.Fatalf("expected success, got %q", res.Message)
	}
}

// TestConfigFetchActionRemoved pins the wire-audit I3 removal: config.fetch
// had a fully implemented agent pipeline whose result no panel code ever
// requested or parsed. The action is gone; a stale delivery now gets the
// standard unsupported-action failure (same as the removed runtime.reload).
func TestConfigFetchActionRemoved(t *testing.T) {
	a := New(Config{}, &fakeTelemtClient{managedConfig: map[string]any{"general": map[string]any{}}, managedRevision: "r7"})
	res := a.HandleJob(context.Background(), &gatewayrpc.JobCommand{
		Id:     "job-config-fetch",
		Action: "config.fetch",
	}, time.Date(2026, time.July, 16, 12, 0, 0, 0, time.UTC))
	if res.Success {
		t.Fatalf("config.fetch succeeded, want unsupported-action failure")
	}
	if !strings.Contains(res.Message, "unsupported action") {
		t.Fatalf("Message = %q, want unsupported action", res.Message)
	}
}

// TestConfigApplyHotReloadUnhealthyRollsBack guards H1: a hot-reload patch
// (RestartRequired=false) that leaves Telemt unhealthy must be rolled back
// (config file restored) and reported as a failure, not a false success —
// previously the hot path returned success immediately with no post-apply
// health check at all.
func TestConfigApplyHotReloadUnhealthyRollsBack(t *testing.T) {
	path := writeTempConfig(t)
	tc := &fakeTelemt{
		patchResult: telemt.PatchConfigResult{Revision: "r2", RestartRequired: false},
		// preflight healthy, then unhealthy after the hot patch, then healthy
		// again once the rollback restores the backup.
		healthSeq: []bool{true, false, false, true},
	}
	res := runConfigApply(context.Background(), configApplyDeps{
		telemt: tc, configPath: path, healthAttempts: 2, healthInterval: time.Millisecond,
	}, configApplyPayload{Patch: map[string]any{"general": map[string]any{"log_level": "debug"}}})

	if res.success {
		t.Fatalf("expected failure on unhealthy hot reload, got success: %q", res.message)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "tls_domain=\"orig\"\n" {
		t.Fatalf("expected backup restore (rollback) on unhealthy hot reload, got %q", got)
	}
	if _, err := os.Stat(path + ".panvex.bak"); !os.IsNotExist(err) {
		t.Fatal("expected .panvex.bak cleaned up")
	}
}

// TestConfigApplyHotReloadHealthy guards the symmetric happy path: a
// hot-reload patch that leaves Telemt healthy must succeed, must not
// restart, and must clean up the backup file.
func TestConfigApplyHotReloadHealthy(t *testing.T) {
	path := writeTempConfig(t)
	tc := &fakeTelemt{
		patchResult: telemt.PatchConfigResult{Revision: "r2", RestartRequired: false},
		healthSeq:   []bool{true, true},
	}
	res := runConfigApply(context.Background(), configApplyDeps{telemt: tc, configPath: path},
		configApplyPayload{Patch: map[string]any{"general": map[string]any{"log_level": "debug"}}})

	if !res.success {
		t.Fatalf("expected success, got %q", res.message)
	}
	if _, err := os.Stat(path + ".panvex.bak"); !os.IsNotExist(err) {
		t.Fatal("expected .panvex.bak cleaned up on hot success path")
	}
}

// TestConfigApplyReloadRollbackSurvivesExpiredJobContext guards the reload-era
// successor of A5: when the job ctx dies mid-health-poll (post-reload), the
// hot rollback (restore + rollback reload, submitted with keep_new) must
// still run to completion on a detached context — otherwise the config file
// is restored on disk while Telemt's running generation is left on the
// broken config.
func TestConfigApplyReloadRollbackSurvivesExpiredJobContext(t *testing.T) {
	path := writeTempConfig(t)
	tc := &fakeTelemt{
		patchResult:     telemt.PatchConfigResult{Revision: "r2", RuntimeReloadRequired: ptrBool(true)},
		submitAccepted:  telemt.ReloadAccepted{ReloadID: 7},
		statusSeq:       []telemt.ReloadStatus{{State: "draining"}}, // both the forward and rollback poll read this (clamped)
		healthSeq:       []bool{true /* preflight */, false, false /* post-reload unhealthy */},
		managedRevision: "r1", // read back by rollbackReload after restoring the file
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // the job deadline is already gone by the time the post-reload health check runs

	res := runConfigApply(ctx, configApplyDeps{
		telemt: tc, configPath: path, healthAttempts: 2, healthInterval: time.Millisecond, reloadPoll: time.Millisecond,
	}, configApplyPayload{Patch: map[string]any{"censorship": map[string]any{"tls_domain": "b"}}, ReloadMode: "instant"})

	if res.success {
		t.Fatalf("expected failure, got success: %q", res.message)
	}
	if !strings.Contains(res.message, "rolled back") {
		t.Fatalf("message = %q, want it to confirm rollback", res.message)
	}
	// Forward submit + rollback's own keep_new submit.
	if tc.submitCalls != 2 {
		t.Fatalf("submitCalls = %d, want 2 (forward reload + rollback keep_new reload)", tc.submitCalls)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "tls_domain=\"orig\"\n" {
		t.Fatalf("config not rolled back on disk: %q", got)
	}
	if _, err := os.Stat(path + ".panvex.bak"); !os.IsNotExist(err) {
		t.Fatal("expected .panvex.bak cleaned up once the rollback-reload succeeded")
	}
}

func ptrBool(b bool) *bool { return &b }

// reload not needed -> hot path, no SubmitReload call
func TestConfigApplyReloadNotRequiredIsHot(t *testing.T) {
	path := writeTempConfig(t)
	tc := &fakeTelemt{
		patchResult: telemt.PatchConfigResult{Revision: "r2", RuntimeReloadRequired: ptrBool(false)},
		healthSeq:   []bool{true /*preflight*/, true /*post*/},
	}
	res := runConfigApply(context.Background(), configApplyDeps{telemt: tc, configPath: path,
		healthAttempts: 1, healthInterval: time.Millisecond}, configApplyPayload{Patch: map[string]any{"general": map[string]any{"log_level": "info"}}})
	if !res.success {
		t.Fatalf("want success, got %q", res.message)
	}
	if tc.submitCalls != 0 {
		t.Fatalf("SubmitReload called %d times, want 0 on a hot change", tc.submitCalls)
	}
}

// reload needed, draining reached -> success WITHOUT waiting for succeeded
func TestConfigApplyReloadSucceedsAtDraining(t *testing.T) {
	path := writeTempConfig(t)
	tc := &fakeTelemt{
		patchResult:    telemt.PatchConfigResult{Revision: "r2", RuntimeReloadRequired: ptrBool(true)},
		submitAccepted: telemt.ReloadAccepted{ReloadID: 7, State: "accepted"},
		statusSeq:      []telemt.ReloadStatus{{State: "preparing"}, {State: "activating"}, {State: "draining"}},
		healthSeq:      []bool{true /*preflight*/, true /*post-activation*/},
	}
	res := runConfigApply(context.Background(), configApplyDeps{telemt: tc, configPath: path,
		healthAttempts: 1, healthInterval: time.Millisecond, reloadPoll: time.Millisecond},
		configApplyPayload{Patch: map[string]any{"censorship": map[string]any{"tls_domain": "x"}}, ReloadMode: "drain", ReloadTimeoutSecs: 30})
	if !res.success {
		t.Fatalf("want success at draining, got %q", res.message)
	}
}

// reload failed -> file restored, job failed
func TestConfigApplyReloadFailedRestoresBackup(t *testing.T) {
	path := writeTempConfig(t)
	tc := &fakeTelemt{
		patchResult:    telemt.PatchConfigResult{Revision: "r2", RuntimeReloadRequired: ptrBool(true)},
		submitAccepted: telemt.ReloadAccepted{ReloadID: 7},
		statusSeq:      []telemt.ReloadStatus{{State: "preparing"}, {State: "failed", Error: "tls-front not ready"}},
		healthSeq:      []bool{true},
	}
	res := runConfigApply(context.Background(), configApplyDeps{telemt: tc, configPath: path,
		reloadPoll: time.Millisecond}, configApplyPayload{Patch: map[string]any{"censorship": map[string]any{"tls_domain": "x"}}, ReloadMode: "instant"})
	if res.success {
		t.Fatalf("want failure on reload failed")
	}
	got, _ := os.ReadFile(path)
	if string(got) != "tls_domain=\"orig\"\n" {
		t.Fatalf("config not restored: %q", got)
	}
}

// 409 reload_in_progress -> retried -> succeeds
func TestConfigApplyReloadBusyRetries(t *testing.T) {
	path := writeTempConfig(t)
	tc := &fakeTelemt{
		patchResult: telemt.PatchConfigResult{Revision: "r2", RuntimeReloadRequired: ptrBool(true)},
		statusSeq:   []telemt.ReloadStatus{{State: "draining"}},
		healthSeq:   []bool{true, true},
	}
	// first submit busy, second accepted
	calls := 0
	tc.submitFn = func() (telemt.ReloadAccepted, error) {
		calls++
		if calls == 1 {
			return telemt.ReloadAccepted{}, telemt.ErrReloadInProgress
		}
		return telemt.ReloadAccepted{ReloadID: 7}, nil
	}
	res := runConfigApply(context.Background(), configApplyDeps{telemt: tc, configPath: path,
		healthAttempts: 1, healthInterval: time.Millisecond, reloadPoll: time.Millisecond,
		reloadBackoff: []time.Duration{time.Millisecond, time.Millisecond, time.Millisecond}},
		configApplyPayload{Patch: map[string]any{"censorship": map[string]any{"tls_domain": "x"}}, ReloadMode: "instant"})
	if !res.success {
		t.Fatalf("want success after one busy retry, got %q", res.message)
	}
	if calls != 2 {
		t.Fatalf("submit calls = %d, want 2", calls)
	}
}

// old Telemt, restart_required true -> restore + fail with upgrade hint
func TestConfigApplyOldTelemtRestartRequiredFails(t *testing.T) {
	path := writeTempConfig(t)
	tc := &fakeTelemt{
		patchResult: telemt.PatchConfigResult{Revision: "r2", RestartRequired: true, RuntimeReloadRequired: nil},
		healthSeq:   []bool{true},
	}
	res := runConfigApply(context.Background(), configApplyDeps{telemt: tc, configPath: path},
		configApplyPayload{Patch: map[string]any{"censorship": map[string]any{"tls_domain": "x"}}})
	if res.success {
		t.Fatalf("want failure on old Telemt restart-required")
	}
	if !strings.Contains(res.message, "3.4.25") {
		t.Fatalf("message should name the version floor: %q", res.message)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "tls_domain=\"orig\"\n" {
		t.Fatalf("config not restored: %q", got)
	}
}

// old Telemt, restart_required false -> hot success + note
func TestConfigApplyOldTelemtHotIsSuccessWithNote(t *testing.T) {
	path := writeTempConfig(t)
	tc := &fakeTelemt{
		patchResult: telemt.PatchConfigResult{Revision: "r2", RestartRequired: false, RuntimeReloadRequired: nil},
		healthSeq:   []bool{true, true},
	}
	res := runConfigApply(context.Background(), configApplyDeps{telemt: tc, configPath: path,
		healthAttempts: 1, healthInterval: time.Millisecond},
		configApplyPayload{Patch: map[string]any{"general": map[string]any{"log_level": "info"}}})
	if !res.success {
		t.Fatalf("want success, got %q", res.message)
	}
}
