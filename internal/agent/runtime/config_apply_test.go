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
	"github.com/lost-coder/panvex/internal/configcanon"
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
}

func (f *fakeTelemt) PatchConfig(_ context.Context, patch map[string]any, expectedRevision string) (telemt.PatchConfigResult, error) {
	f.patchCalls++
	f.patchedWith = patch
	f.patchedRev = expectedRevision
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
	rest := &fakeRestarter{}
	res := runConfigApply(context.Background(), configApplyDeps{telemt: tc, restarter: rest, configPath: path},
		configApplyPayload{Patch: map[string]any{"general": map[string]any{"log_level": "debug"}}})
	if !res.success {
		t.Fatalf("expected success, got %q", res.message)
	}
	if rest.restarts != 0 {
		t.Fatalf("hot change must not restart, got %d", rest.restarts)
	}
}

func TestConfigApplyRestartHealthy(t *testing.T) {
	path := writeTempConfig(t)
	tc := &fakeTelemt{patchResult: telemt.PatchConfigResult{Revision: "r2", RestartRequired: true}, healthSeq: []bool{true}}
	rest := &fakeRestarter{}
	res := runConfigApply(context.Background(), configApplyDeps{telemt: tc, restarter: rest, configPath: path},
		configApplyPayload{Patch: map[string]any{"censorship": map[string]any{"tls_domain": "b"}}})
	if !res.success {
		t.Fatalf("expected success, got %q", res.message)
	}
	if rest.restarts != 1 {
		t.Fatalf("expected 1 restart, got %d", rest.restarts)
	}
}

func TestConfigApplyRestartUnhealthyRollsBack(t *testing.T) {
	path := writeTempConfig(t)
	tc := &fakeTelemt{patchResult: telemt.PatchConfigResult{Revision: "r2", RestartRequired: true}, healthSeq: []bool{true /*preflight*/, false, false}}
	rest := &fakeRestarter{}
	res := runConfigApply(context.Background(), configApplyDeps{
		telemt: tc, restarter: rest, configPath: path, healthAttempts: 2, healthInterval: time.Millisecond,
	}, configApplyPayload{Patch: map[string]any{"censorship": map[string]any{"tls_domain": "b"}}})
	if res.success {
		t.Fatalf("expected failure on unhealthy restart")
	}
	if rest.restarts < 2 {
		t.Fatalf("expected restart + rollback restart (>=2), got %d", rest.restarts)
	}
}

func TestConfigApplyRestartRequiredButNoRestarter(t *testing.T) {
	path := writeTempConfig(t)
	tc := &fakeTelemt{patchResult: telemt.PatchConfigResult{Revision: "r2", RestartRequired: true}}
	res := runConfigApply(context.Background(), configApplyDeps{telemt: tc, restarter: nil, configPath: path},
		configApplyPayload{Patch: map[string]any{"censorship": map[string]any{"tls_domain": "b"}}})
	if res.success {
		t.Fatalf("expected failure when restart required but no restarter")
	}
}

func TestConfigApplyPreflightUnhealthyAborts(t *testing.T) {
	path := writeTempConfig(t)
	tc := &fakeTelemt{healthSeq: []bool{false}} // preflight sees unhealthy
	rest := &fakeRestarter{}
	res := runConfigApply(context.Background(), configApplyDeps{telemt: tc, restarter: rest, configPath: path},
		configApplyPayload{Patch: map[string]any{"censorship": map[string]any{"tls_domain": "b"}}})
	if res.success {
		t.Fatalf("expected failure when Telemt is unhealthy at preflight")
	}
	if tc.patchedWith != nil {
		t.Fatalf("patch must NOT be attempted when preflight fails")
	}
	if rest.restarts != 0 {
		t.Fatalf("no restart expected, got %d", rest.restarts)
	}
}

func TestConfigApplyRevisionConflictNoRestart(t *testing.T) {
	path := writeTempConfig(t)
	tc := &fakeTelemt{patchErr: telemt.ErrConfigRevisionConflict, healthSeq: []bool{true}}
	rest := &fakeRestarter{}
	res := runConfigApply(context.Background(), configApplyDeps{telemt: tc, restarter: rest, configPath: path},
		configApplyPayload{ExpectedRevision: "stale", Patch: map[string]any{"censorship": map[string]any{"tls_domain": "b"}}})
	if res.success {
		t.Fatalf("expected failure on revision conflict")
	}
	if rest.restarts != 0 {
		t.Fatalf("no restart on patch error, got %d", rest.restarts)
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
	rest := &fakeRestarter{}
	res := runConfigApply(context.Background(), configApplyDeps{telemt: tc, restarter: rest, configPath: path},
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
	rest := &fakeRestarter{}
	res := runConfigApply(context.Background(), configApplyDeps{telemt: tc, restarter: rest, configPath: path},
		configApplyPayload{Patch: map[string]any{"general": map[string]any{"log_level": "debug"}}})

	if res.success {
		t.Fatalf("expected conflict/no-op, got success (blind re-apply): %q", res.message)
	}
	if tc.patchCalls != 1 {
		t.Fatalf("expected exactly 1 PatchConfig attempt (no blind retry/double-apply), got %d", tc.patchCalls)
	}
	if rest.restarts != 0 {
		t.Fatalf("no restart expected on a conflicted duplicate apply, got %d", rest.restarts)
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

func TestHandleConfigFetchJob(t *testing.T) {
	sections := map[string]any{"general": map[string]any{"log_level": "debug"}}
	a := New(Config{}, &fakeTelemtClient{managedConfig: sections, managedRevision: "r7"})
	res := a.handleConfigFetchJob(context.Background(), &gatewayrpc.JobResult{})
	if !res.Success {
		t.Fatalf("expected success, got %q", res.Message)
	}
	if !strings.Contains(res.ResultJson, `"log_level":"debug"`) {
		t.Fatalf("ResultJson missing sections: %q", res.ResultJson)
	}
	if !strings.Contains(res.ResultJson, `"revision":"r7"`) {
		t.Fatalf("ResultJson missing revision: %q", res.ResultJson)
	}
	if want := configcanon.Hash(sections); !strings.Contains(res.ResultJson, want) {
		t.Fatalf("ResultJson missing hash %q: %q", want, res.ResultJson)
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
	rest := &fakeRestarter{}
	res := runConfigApply(context.Background(), configApplyDeps{
		telemt: tc, restarter: rest, configPath: path, healthAttempts: 2, healthInterval: time.Millisecond,
	}, configApplyPayload{Patch: map[string]any{"general": map[string]any{"log_level": "debug"}}})

	if res.success {
		t.Fatalf("expected failure on unhealthy hot reload, got success: %q", res.message)
	}
	if rest.restarts != 0 {
		t.Fatalf("hot rollback must not restart Telemt, got %d restarts", rest.restarts)
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
	rest := &fakeRestarter{}
	res := runConfigApply(context.Background(), configApplyDeps{telemt: tc, restarter: rest, configPath: path},
		configApplyPayload{Patch: map[string]any{"general": map[string]any{"log_level": "debug"}}})

	if !res.success {
		t.Fatalf("expected success, got %q", res.message)
	}
	if rest.restarts != 0 {
		t.Fatalf("hot change must not restart, got %d", rest.restarts)
	}
	if _, err := os.Stat(path + ".panvex.bak"); !os.IsNotExist(err) {
		t.Fatal("expected .panvex.bak cleaned up on hot success path")
	}
}

// TestConfigApplyRollbackSurvivesExpiredJobContext guards A5: when the job
// ctx dies mid-health-poll, the rollback (restore + restart) must still run
// to completion on a detached context — otherwise the config file is
// restored on disk while Telemt keeps running the unverified config.
func TestConfigApplyRollbackSurvivesExpiredJobContext(t *testing.T) {
	path := writeTempConfig(t)
	tc := &fakeTelemt{
		patchResult: telemt.PatchConfigResult{Revision: "r2", RestartRequired: true},
		healthSeq:   []bool{true /* preflight */, false, false},
	}
	rest := &fakeRestarter{}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // the job deadline is already gone when the apply runs

	res := runConfigApply(ctx, configApplyDeps{
		telemt: tc, restarter: rest, configPath: path, healthAttempts: 2, healthInterval: time.Millisecond,
	}, configApplyPayload{Patch: map[string]any{"censorship": map[string]any{"tls_domain": "b"}}})

	if res.success {
		t.Fatalf("expected failure, got success: %q", res.message)
	}
	if rest.restarts != 2 {
		t.Fatalf("expected forward restart + rollback restart, got %d", rest.restarts)
	}
	// Forward restart saw the dead job ctx; the rollback restart must have
	// received a LIVE detached ctx.
	if rest.restartCtxErrs[0] == nil {
		t.Fatal("test setup broken: forward restart should observe the cancelled job ctx")
	}
	if rest.restartCtxErrs[1] != nil {
		t.Fatalf("rollback restart ctx must be alive (detached), observed err: %v", rest.restartCtxErrs[1])
	}
	got, _ := os.ReadFile(path)
	if string(got) != "tls_domain=\"orig\"\n" {
		t.Fatalf("config not rolled back on disk: %q", got)
	}
}

// TestConfigApplyFailedRestartRollsBackAndRestarts guards A5: a failed
// forward restart must restore the previous config AND restart Telemt on it
// — previously only the file was restored, leaving the process down or on
// stale state.
func TestConfigApplyFailedRestartRollsBackAndRestarts(t *testing.T) {
	path := writeTempConfig(t)
	tc := &fakeTelemt{patchResult: telemt.PatchConfigResult{Revision: "r2", RestartRequired: true}}
	rest := &fakeRestarter{restartErrSeq: []error{errors.New("unit failed"), nil}}

	res := runConfigApply(context.Background(), configApplyDeps{telemt: tc, restarter: rest, configPath: path},
		configApplyPayload{Patch: map[string]any{"censorship": map[string]any{"tls_domain": "b"}}})

	if res.success {
		t.Fatalf("expected failure, got success: %q", res.message)
	}
	if rest.restarts != 2 {
		t.Fatalf("expected failed forward restart + rollback restart, got %d", rest.restarts)
	}
	if !strings.Contains(res.message, "rolled back") {
		t.Fatalf("message = %q, want it to confirm rollback", res.message)
	}
}
