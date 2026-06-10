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
}

func (f *fakeTelemt) PatchConfig(_ context.Context, patch map[string]any, _ string) (telemt.PatchConfigResult, error) {
	f.patchedWith = patch
	return f.patchResult, f.patchErr
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

func (f *fakeRestarter) Verify(context.Context) error { return nil }
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
