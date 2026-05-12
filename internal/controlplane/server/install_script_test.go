package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
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

// TestInstallScriptSHA256_StableHash exercises the cached hash helper. The
// digest must be stable across calls (sync.Once) and must equal a freshly
// computed SHA-256 over the embedded body — guards against a regression that
// e.g. accidentally hashes a copy or applies a transformation. (S-3.)
func TestInstallScriptSHA256_StableHash(t *testing.T) {
	t.Parallel()
	h1, err := installScriptSHA256()
	if err != nil {
		t.Fatalf("installScriptSHA256: %v", err)
	}
	h2, err := installScriptSHA256()
	if err != nil {
		t.Fatalf("installScriptSHA256 second call: %v", err)
	}
	if h1 != h2 {
		t.Fatalf("hash not stable: %s vs %s", h1, h2)
	}
	want := sha256.Sum256(installScriptBytes)
	if hex.EncodeToString(want[:]) != h1 {
		t.Fatalf("hash mismatch: got %s, want %s", h1, hex.EncodeToString(want[:]))
	}
	if len(h1) != 64 {
		t.Fatalf("expected 64 hex chars, got %d (%q)", len(h1), h1)
	}
}

// TestServeInstallScript_AdvertisesSHA256Header asserts the install-script
// handler emits X-Install-Script-SHA256 carrying the body's lowercase hex
// digest. The header is what the bootstrap install-command embeds into the
// curl|bash one-liner so the script self-verifies (T-5). (S-3.)
func TestServeInstallScript_AdvertisesSHA256Header(t *testing.T) {
	t.Parallel()
	srv := &Server{}
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/install-agent.sh", nil)
	rec := httptest.NewRecorder()

	srv.handleInstallAgentScript()(rec, req)

	got := rec.Header().Get("X-Install-Script-SHA256")
	if len(got) != 64 {
		t.Fatalf("expected 64-char hex, got %q (len=%d)", got, len(got))
	}
	want := sha256.Sum256(installAgentScript)
	if got != hex.EncodeToString(want[:]) {
		t.Fatalf("hash header mismatch: got %s, want %s", got, hex.EncodeToString(want[:]))
	}
	cc := rec.Header().Get("Cache-Control")
	if !strings.Contains(cc, "must-revalidate") {
		t.Fatalf("Cache-Control = %q, want must-revalidate", cc)
	}
}

func minLen(a, b int) int {
	if a < b {
		return a
	}
	return b
}
