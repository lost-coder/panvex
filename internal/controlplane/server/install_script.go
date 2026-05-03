package server

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"net/http"
	"strconv"
	"sync"
)

// installAgentScript is the canonical bash installer that operators pipe into
// `sudo bash` after retrieving the per-agent install command from
// /api/agents/{id}/install-command. The control-plane serves it directly so
// `curl <panel>/install-agent.sh | sudo bash -s -- ...` works without a
// dependency on an external CDN — the panel is its own distribution channel.
//
//go:embed install_agent.sh
var installAgentScript []byte

// Alias exposed for tests; matches the variable name used in the plan's
// reference test snippet (S-3, T3).
var installScriptBytes = installAgentScript

// installScriptHashOnce memoizes the hex-encoded SHA-256 of installAgentScript.
// The script body is immutable for the lifetime of the binary (it is embedded
// at build time via go:embed), so a single hash is computed once and reused
// on every request. (S-3.)
var (
	installScriptHashOnce sync.Once
	installScriptHashHex  string
	installScriptHashErr  error
)

// installScriptSHA256 returns the lowercase hex SHA-256 digest of the embedded
// install-agent.sh body. The first call computes the hash; subsequent calls
// return the cached value. The error return exists for symmetry with
// hash-producing helpers elsewhere in the codebase — sha256.Sum256 itself is
// infallible, so err is always nil today, but keeping the slot lets future
// implementations (e.g. read-from-disk during dev) surface failures without a
// signature change. (S-3.)
func installScriptSHA256() (string, error) {
	installScriptHashOnce.Do(func() {
		sum := sha256.Sum256(installAgentScript)
		installScriptHashHex = hex.EncodeToString(sum[:])
	})
	return installScriptHashHex, installScriptHashErr
}

// InstallScriptSHA256 is the exported accessor for the install-script digest.
// cmd/control-plane wires it into bootstrap.InstallCommandConfig.ScriptHash so
// the generated curl|bash one-liner pins the body the panel currently serves.
// On the unreachable error path it returns "" — callers should treat that as
// "verification disabled" (consistent with the empty ScriptHash contract on
// InstallCommandConfig). (S-3.)
func InstallScriptSHA256() string {
	hash, err := installScriptSHA256()
	if err != nil {
		return ""
	}
	return hash
}

// handleInstallAgentScript serves the embedded install-agent.sh script.
// Mounted at root path /install-agent.sh (NOT under /api/) because the
// generated install-command uses the bare panel URL — operators copy the
// `curl ... | bash` line from the dashboard and paste it on the agent host.
//
// The endpoint is deliberately unauthenticated:
//   - the install command itself carries the bootstrap token (single-use,
//     5-minute TTL — see Sprint S-1 §S-02);
//   - the script body has no per-agent secret;
//   - making it auth-required would force the agent host to obtain a panel
//     session before the agent could enroll, defeating the whole "one-liner"
//     UX.
//
// (Q-05.)
//
// X-Install-Script-SHA256 advertises the lowercase hex SHA-256 of the body so
// operators (and the install-command generator in bootstrap) can pin the
// expected hash. The script self-verifies against PANVEX_INSTALL_SCRIPT_SHA256
// before any state-changing operation, which closes the curl|bash MITM hole.
// (S-3.)
func (s *Server) handleInstallAgentScript() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		h := w.Header()
		h.Set("Content-Type", "text/x-shellscript; charset=utf-8")
		h.Set("Content-Length", strconv.Itoa(len(installAgentScript)))
		// Stable script body — operators may pin a specific panel version
		// for reproducible installs. Cache for 5 minutes; release of a new
		// panel version invalidates downstream caches via the response body
		// hash that operators pin in CI. must-revalidate forces caches to
		// re-check once the max-age window expires rather than serving a
		// stale body that could mismatch the advertised SHA256. (S-3.)
		h.Set("Cache-Control", "public, max-age=300, must-revalidate")
		if hash, err := installScriptSHA256(); err == nil {
			h.Set("X-Install-Script-SHA256", hash)
		}
		_, _ = w.Write(installAgentScript)
	}
}
