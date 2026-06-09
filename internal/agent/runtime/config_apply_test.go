package runtime

import (
	"context"
	"os"
	"path/filepath"
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
	restartErr error
	restarts   int
}

func (f *fakeRestarter) Verify(context.Context) error { return nil }
func (f *fakeRestarter) Restart(context.Context) error {
	f.restarts++
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
