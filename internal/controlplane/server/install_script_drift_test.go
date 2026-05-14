package server

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// deployInstallAgentScriptRelPath is the path from this package directory
// to the canonical install-agent.sh in deploy/. Kept as a constant so the
// drift test's failure message can quote it verbatim.
const deployInstallAgentScriptRelPath = "../../../deploy/install-agent.sh"

// TestInstallAgentScriptEmbeddedMatchesDeployCopy guards the single-source-of-
// truth invariant: deploy/install-agent.sh is the canonical script (it is
// what the GitHub-raw URL serves and what operators editing the file
// reach for), and the package-local mirror is a //go:generate-produced copy.
// If somebody hand-edits the mirror or forgets to re-run `go generate`
// after touching deploy/, this test fails with an actionable message.
//
// We intentionally do NOT auto-copy here: a CI failure is the right
// signal, not a silent fix that hides a drifted commit.
func TestInstallAgentScriptEmbeddedMatchesDeployCopy(t *testing.T) {
	t.Parallel()
	canonical, err := os.ReadFile(filepath.FromSlash(deployInstallAgentScriptRelPath))
	if err != nil {
		t.Fatalf("read %s: %v", deployInstallAgentScriptRelPath, err)
	}
	if !bytes.Equal(canonical, installAgentScript) {
		t.Fatalf(
			"embedded install-agent.sh has drifted from %s\n"+
				"  embedded bytes:  %d\n"+
				"  canonical bytes: %d\n"+
				"Fix: edit deploy/install-agent.sh (the canonical copy), then run\n"+
				"  go generate ./internal/controlplane/server/...\n"+
				"to refresh the mirror. Do NOT edit internal/controlplane/server/install_agent.sh "+
				"directly — it is .gitignored and overwritten by go generate.",
			deployInstallAgentScriptRelPath, len(installAgentScript), len(canonical),
		)
	}
}
