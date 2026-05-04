package server

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

// TestInstallScript_HashMatchesServedBody guards the byte-exact contract
// between the embedded install-agent.sh body and the digest exported via
// InstallScriptSHA256. The bootstrap install-command embeds the digest into
// the curl|bash one-liner so the operator's shell can verify the downloaded
// body before executing it; if the panel's hash drifts from the served body
// (e.g. someone hashes a copy with a transformation, or a future refactor
// hashes a trimmed body) every reverse-mode enrollment fails with a hash
// mismatch.
//
// The HIGH-1 review finding that prompted this test was: a previous version
// of the install-command captured `SCRIPT=$(curl ...)`, which strips trailing
// newlines under POSIX `$()` semantics, then hashed the trimmed body — but
// the panel hashed the embedded file as-is (with its trailing \n). This test
// pins the panel side of that contract: whatever bytes the panel serves must
// match what InstallScriptSHA256() advertises. (S-3.)
func TestInstallScript_HashMatchesServedBody(t *testing.T) {
	t.Parallel()
	body := installScriptBytes
	if len(body) == 0 {
		t.Fatal("installScriptBytes is empty — embed path may have drifted")
	}
	sum := sha256.Sum256(body)
	expected := hex.EncodeToString(sum[:])
	if got := InstallScriptSHA256(); got != expected {
		t.Fatalf("InstallScriptSHA256() = %s, but sha256(installScriptBytes) = %s — drift between served body and advertised digest",
			got, expected)
	}
}
