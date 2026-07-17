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
)

// TestConfigApplyRestartRequiredNoRestarterRestoreFails guards D1/D2#1: the
// audit-named branch where a restart-required change lands with no restart
// strategy configured, so the agent must revert the config file itself. The
// old code discarded restoreConfigFile's error (`_ = restoreConfigFile(...)`)
// and unconditionally reported "reverted" while an unconditional defer
// deleted the backup regardless — a failed restore left the live config
// modified, the backup gone, and the control-plane told the rollback
// succeeded.
//
// The restore is made to fail for REAL (not via a stubbed error): PatchConfig
// chmods the config's directory read-only as a side effect, at the exact
// point production code sits between "backup taken" and "restore attempted",
// so restoreConfigFile's underlying atomicfile.Write (os.CreateTemp in that
// directory) fails on its own.
func TestConfigApplyRestartRequiredNoRestarterRestoreFails(t *testing.T) {
	if os.Geteuid() == 0 {
		// A 0o555 directory does not stop root from creating files in it, so
		// this fault-injection would silently be a no-op and the test would
		// pass without ever exercising the restore-failure path.
		t.Skip("running as root: chmod 0o555 does not block root writes, skipping fault-injection test")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("tls_domain=\"orig\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Let t.TempDir's own cleanup succeed even though the test locks the dir.
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) }) //nolint:gosec // G302: dir must stay traversable (+x); restoring test fixture perms, not production data

	tc := &fakeTelemt{
		patchResult: telemt.PatchConfigResult{Revision: "r2", RestartRequired: true},
		onPatch: func() {
			if err := os.Chmod(dir, 0o555); err != nil { //nolint:gosec // G302: intentional fault injection — makes the dir read-only+traversable so the restore write fails for real
				t.Fatalf("chmod dir read-only: %v", err)
			}
		},
	}

	res := runConfigApply(context.Background(), configApplyDeps{telemt: tc, restarter: nil, configPath: path},
		configApplyPayload{Patch: map[string]any{"censorship": map[string]any{"tls_domain": "b"}}})

	if res.success {
		t.Fatalf("expected failure when the restore itself fails, got success: %q", res.message)
	}
	backup := path + ".panvex.bak"
	if !strings.Contains(res.message, backup) {
		t.Fatalf("message = %q, want it to name the backup path %q", res.message, backup)
	}
	if !strings.Contains(res.message, "manual recovery") {
		t.Fatalf("message = %q, want it to signal manual recovery is required", res.message)
	}
	if _, err := os.Stat(backup); err != nil {
		t.Fatalf("expected the backup to survive a failed restore (only recovery artifact left), stat error: %v", err)
	}
}

// TestConfigApplyRestartRequiredNoRestarterRestoreOK is the mirror image: when
// the restore actually succeeds, the message says reverted, the live config
// is back to the old bytes, and the backup is gone. This pins the other half
// of D1's invariant — "delete the backup only on a known-state path" — so a
// later fix does not overcorrect into always keeping the backup around.
func TestConfigApplyRestartRequiredNoRestarterRestoreOK(t *testing.T) {
	path := writeTempConfig(t)
	tc := &fakeTelemt{patchResult: telemt.PatchConfigResult{Revision: "r2", RestartRequired: true}}

	res := runConfigApply(context.Background(), configApplyDeps{telemt: tc, restarter: nil, configPath: path},
		configApplyPayload{Patch: map[string]any{"censorship": map[string]any{"tls_domain": "b"}}})

	if res.success {
		t.Fatalf("expected failure (restart required, no restarter configured), got success: %q", res.message)
	}
	if !strings.Contains(res.message, "reverted") {
		t.Fatalf("message = %q, want it to say reverted", res.message)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "tls_domain=\"orig\"\n" {
		t.Fatalf("live config not restored to old bytes: %q", got)
	}
	if _, err := os.Stat(path + ".panvex.bak"); !os.IsNotExist(err) {
		t.Fatal("expected backup removed after a successful restore (known-state path)")
	}
}

// TestConfigApplyHotReloadUnhealthyRollbackFails guards D2#2: the hot-reload
// path already told the truth about a failed rollback (the message included
// "rollback failed"), but the unconditional defer still deleted the backup —
// the only recovery artifact — out from under that honest failure message.
// The restore is failed for real via the same directory-chmod technique.
func TestConfigApplyHotReloadUnhealthyRollbackFails(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: chmod 0o555 does not block root writes, skipping fault-injection test")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("tls_domain=\"orig\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) }) //nolint:gosec // G302: dir must stay traversable (+x); restoring test fixture perms, not production data

	tc := &fakeTelemt{
		patchResult: telemt.PatchConfigResult{Revision: "r2", RestartRequired: false},
		// preflight healthy, then unhealthy after the hot patch (both attempts
		// exhausted) — the ensuing hotRollbackConfig then tries to restore the
		// backup and must fail because the directory is now read-only.
		healthSeq: []bool{true, false, false},
		onPatch: func() {
			if err := os.Chmod(dir, 0o555); err != nil { //nolint:gosec // G302: intentional fault injection — makes the dir read-only+traversable so the restore write fails for real
				t.Fatalf("chmod dir read-only: %v", err)
			}
		},
	}
	rest := &fakeRestarter{}

	res := runConfigApply(context.Background(), configApplyDeps{
		telemt: tc, restarter: rest, configPath: path, healthAttempts: 2, healthInterval: time.Millisecond,
	}, configApplyPayload{Patch: map[string]any{"general": map[string]any{"log_level": "debug"}}})

	if res.success {
		t.Fatalf("expected failure, got success: %q", res.message)
	}
	backup := path + ".panvex.bak"
	if !strings.Contains(res.message, "rollback failed") {
		t.Fatalf("message = %q, want it to report the rollback failure", res.message)
	}
	if !strings.Contains(res.message, backup) {
		t.Fatalf("message = %q, want it to name the backup path %q", res.message, backup)
	}
	if !strings.Contains(res.message, "manual recovery") {
		t.Fatalf("message = %q, want it to signal manual recovery is required", res.message)
	}
	if _, err := os.Stat(backup); err != nil {
		t.Fatalf("expected the backup to survive a failed rollback, stat error: %v", err)
	}
	if rest.restarts != 0 {
		t.Fatalf("hot rollback must not restart Telemt, got %d restarts", rest.restarts)
	}
}

// TestConfigApplyPostRestartUnhealthyRollbackFails guards D2#3: the
// post-restart-unhealthy path already reported the rollback failure in its
// message, but the unconditional defer still deleted the backup. Restart
// succeeds, health stays down, and the ensuing rollbackConfig's restore is
// failed for real via the directory-chmod technique.
func TestConfigApplyPostRestartUnhealthyRollbackFails(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: chmod 0o555 does not block root writes, skipping fault-injection test")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("tls_domain=\"orig\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) }) //nolint:gosec // G302: dir must stay traversable (+x); restoring test fixture perms, not production data

	tc := &fakeTelemt{
		patchResult: telemt.PatchConfigResult{Revision: "r2", RestartRequired: true},
		healthSeq:   []bool{true /* preflight */, false, false},
		onPatch: func() {
			if err := os.Chmod(dir, 0o555); err != nil { //nolint:gosec // G302: intentional fault injection — makes the dir read-only+traversable so the restore write fails for real
				t.Fatalf("chmod dir read-only: %v", err)
			}
		},
	}
	rest := &fakeRestarter{}

	res := runConfigApply(context.Background(), configApplyDeps{
		telemt: tc, restarter: rest, configPath: path, healthAttempts: 2, healthInterval: time.Millisecond,
	}, configApplyPayload{Patch: map[string]any{"censorship": map[string]any{"tls_domain": "b"}}})

	if res.success {
		t.Fatalf("expected failure, got success: %q", res.message)
	}
	backup := path + ".panvex.bak"
	if !strings.Contains(res.message, backup) {
		t.Fatalf("message = %q, want it to name the backup path %q", res.message, backup)
	}
	if !strings.Contains(res.message, "manual recovery") {
		t.Fatalf("message = %q, want it to signal manual recovery is required", res.message)
	}
	if _, err := os.Stat(backup); err != nil {
		t.Fatalf("expected the backup to survive a failed rollback, stat error: %v", err)
	}
	if rest.restarts != 1 {
		t.Fatalf("expected exactly the forward restart (rollback's own restart never reached since restore failed first), got %d", rest.restarts)
	}
}

// TestConfigApplyRestartFailedRollbackFails guards the remaining rbErr branch
// covered by the same "keep backup on any failed rollback" rule as D2#3: the
// forward restart itself fails, and the ensuing rollbackConfig's restore also
// fails for real. Both the restart failure and the rollback failure are
// already reported in the message; the backup must still be kept.
func TestConfigApplyRestartFailedRollbackFails(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: chmod 0o555 does not block root writes, skipping fault-injection test")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte("tls_domain=\"orig\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) }) //nolint:gosec // G302: dir must stay traversable (+x); restoring test fixture perms, not production data

	tc := &fakeTelemt{
		patchResult: telemt.PatchConfigResult{Revision: "r2", RestartRequired: true},
		onPatch: func() {
			if err := os.Chmod(dir, 0o555); err != nil { //nolint:gosec // G302: intentional fault injection — makes the dir read-only+traversable so the restore write fails for real
				t.Fatalf("chmod dir read-only: %v", err)
			}
		},
	}
	rest := &fakeRestarter{restartErrSeq: []error{errors.New("unit failed")}}

	res := runConfigApply(context.Background(), configApplyDeps{telemt: tc, restarter: rest, configPath: path},
		configApplyPayload{Patch: map[string]any{"censorship": map[string]any{"tls_domain": "b"}}})

	if res.success {
		t.Fatalf("expected failure, got success: %q", res.message)
	}
	backup := path + ".panvex.bak"
	if !strings.Contains(res.message, backup) {
		t.Fatalf("message = %q, want it to name the backup path %q", res.message, backup)
	}
	if !strings.Contains(res.message, "manual recovery") {
		t.Fatalf("message = %q, want it to signal manual recovery is required", res.message)
	}
	if _, err := os.Stat(backup); err != nil {
		t.Fatalf("expected the backup to survive a failed rollback, stat error: %v", err)
	}
	if rest.restarts != 1 {
		t.Fatalf("expected exactly 1 restart attempt (the failed forward one), got %d", rest.restarts)
	}
}
