package main

import (
	"os/exec"
	"strings"
	"testing"
)

// TestAgentDoesNotImportControlPlaneServerOrDB guards R3 (audit 2026-07-07 §4):
// the agent binary must not transitively import the control-plane server
// package, the storage layer, or either DB driver. The agent downloads itself
// on self-update, so this isolation keeps that binary small and prevents the
// control-plane's heavy dependencies from leaking into it. Uses `go list`
// rather than an AST walk because transitivity is what matters here and
// `go list -deps` computes it directly.
func TestAgentDoesNotImportControlPlaneServerOrDB(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping go-list dependency guard in -short mode")
	}
	out, err := exec.CommandContext(t.Context(), "go", "list", "-deps", "github.com/lost-coder/panvex/cmd/agent").Output()
	if err != nil {
		t.Skipf("go list unavailable (%v); skipping dependency guard", err)
	}

	forbiddenPrefixes := []string{
		"github.com/lost-coder/panvex/internal/controlplane/server",
		"github.com/lost-coder/panvex/internal/controlplane/storage",
		"modernc.org/sqlite",
		"github.com/jackc/pgx/v5",
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		dep := strings.TrimSpace(line)
		for _, prefix := range forbiddenPrefixes {
			if dep == prefix || strings.HasPrefix(dep, prefix+"/") {
				t.Errorf("cmd/agent must not depend on %q (R3 isolation)", dep)
			}
		}
	}
}
