//go:build !windows

package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// writeEmbeddedScript materializes the embedded install-agent.sh body to a
// tempfile so tests can exec it from a real path; the self-check reads its
// own bytes via ${BASH_SOURCE[0]}, which only works when the script lives on
// disk. The returned path is removed by t.Cleanup.
func writeEmbeddedScript(t *testing.T) (string, []byte) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "install-agent.sh")
	body := installScriptBytes
	if len(body) == 0 {
		t.Fatal("installScriptBytes empty — embed broken")
	}
	if err := os.WriteFile(path, body, 0o755); err != nil {
		t.Fatalf("write tempfile: %v", err)
	}
	return path, body
}

// runScript runs `bash <path> --help` with the given env extra; --help exits 0
// after the self-check, so we can use the exit code to decide whether the
// self-check accepted or rejected the body.
func runScript(t *testing.T, path string, env ...string) (int, string, string) {
	t.Helper()
	cmd := exec.Command("bash", path, "--help")
	cmd.Env = append(os.Environ(), env...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	exit := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			exit = ee.ExitCode()
		} else {
			t.Fatalf("exec bash: %v", err)
		}
	}
	return exit, stdout.String(), stderr.String()
}

// TestInstallAgentScript_SelfCheckPassesOnMatchingHash verifies that when
// PANVEX_INSTALL_SCRIPT_SHA256 matches the script body's digest, the self
// check accepts and the script proceeds (--help exits 0). (T-5.)
func TestInstallAgentScript_SelfCheckPassesOnMatchingHash(t *testing.T) {
	path, body := writeEmbeddedScript(t)
	sum := sha256.Sum256(body)
	expected := hex.EncodeToString(sum[:])

	exit, stdout, stderr := runScript(t, path, "PANVEX_INSTALL_SCRIPT_SHA256="+expected)
	if exit != 0 {
		t.Fatalf("expected exit 0 with matching hash; got %d. stderr=%q", exit, stderr)
	}
	if !strings.Contains(stdout, "Panvex Agent") {
		t.Fatalf("expected --help banner on stdout, got: %q", stdout)
	}
	if strings.Contains(stderr, "self-check failed") {
		t.Fatalf("unexpected self-check failure in stderr: %q", stderr)
	}
}

// TestInstallAgentScript_SelfCheckFailsOnMismatchedHash verifies that a wrong
// PANVEX_INSTALL_SCRIPT_SHA256 causes the script to abort before any other
// work. (T-5.)
func TestInstallAgentScript_SelfCheckFailsOnMismatchedHash(t *testing.T) {
	path, _ := writeEmbeddedScript(t)
	bogus := strings.Repeat("0", 64)

	exit, _, stderr := runScript(t, path, "PANVEX_INSTALL_SCRIPT_SHA256="+bogus)
	if exit == 0 {
		t.Fatal("expected non-zero exit on mismatched hash; got 0")
	}
	if !strings.Contains(stderr, "self-check failed") {
		t.Fatalf("expected stderr to mention self-check failure, got: %q", stderr)
	}
	if !strings.Contains(stderr, bogus) {
		t.Fatalf("expected stderr to echo the expected hash, got: %q", stderr)
	}
}

// TestInstallAgentScript_SelfCheckSkippedWhenEnvUnset verifies that without the
// env var, the script behaves exactly as before — the self-check is opt-in
// and must not affect operators who haven't pinned a hash. (T-5.)
func TestInstallAgentScript_SelfCheckSkippedWhenEnvUnset(t *testing.T) {
	path, _ := writeEmbeddedScript(t)

	// Build an env that explicitly drops PANVEX_INSTALL_SCRIPT_SHA256 in case
	// it was inherited from the test runner.
	clean := make([]string, 0, len(os.Environ()))
	for _, e := range os.Environ() {
		if strings.HasPrefix(e, "PANVEX_INSTALL_SCRIPT_SHA256=") {
			continue
		}
		clean = append(clean, e)
	}
	cmd := exec.Command("bash", path, "--help")
	cmd.Env = clean
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("expected exit 0 with no hash set; got err=%v stderr=%q", err, stderr.String())
	}
	if strings.Contains(stderr.String(), "self-check") {
		t.Fatalf("self-check should be silent when env unset; stderr=%q", stderr.String())
	}
}
