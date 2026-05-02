package server

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestInstallAgentScriptEmbedded asserts the bash installer is compiled into
// the binary and is non-empty. Catches a build-time regression where the
// embed path diverges from the on-disk file. (Q-05)
func TestInstallAgentScriptEmbedded(t *testing.T) {
	t.Parallel()
	if len(installAgentScript) == 0 {
		t.Fatal("installAgentScript is empty — go:embed path may have drifted from install_agent.sh")
	}
	// Sanity: the script begins with a shebang line. Cheap check that the
	// embed picked up a real bash script and not, say, an empty file or HTML.
	if !bytes.HasPrefix(installAgentScript, []byte("#!/usr/bin/env bash\n")) &&
		!bytes.HasPrefix(installAgentScript, []byte("#!/bin/bash\n")) {
		t.Fatalf("installAgentScript does not begin with a bash shebang: %q",
			string(installAgentScript[:minLen(64, len(installAgentScript))]))
	}
}

// TestInstallAgentScriptHandlerServesEmbeddedBody asserts the HTTP handler
// returns the embedded script verbatim with a shell-script Content-Type.
// (Q-05)
func TestInstallAgentScriptHandlerServesEmbeddedBody(t *testing.T) {
	t.Parallel()
	srv := &Server{}
	rr := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/install-agent.sh", nil)

	srv.handleInstallAgentScript()(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	gotCT := rr.Header().Get("Content-Type")
	if !strings.HasPrefix(gotCT, "text/x-shellscript") {
		t.Fatalf("Content-Type = %q, want text/x-shellscript prefix", gotCT)
	}
	if !bytes.Equal(rr.Body.Bytes(), installAgentScript) {
		t.Fatalf("body length = %d, want %d (embedded script bytes)",
			rr.Body.Len(), len(installAgentScript))
	}
}

func minLen(a, b int) int {
	if a < b {
		return a
	}
	return b
}
