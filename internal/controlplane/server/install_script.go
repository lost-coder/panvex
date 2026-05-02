package server

import (
	_ "embed"
	"net/http"
	"strconv"
)

// installAgentScript is the canonical bash installer that operators pipe into
// `sudo bash` after retrieving the per-agent install command from
// /api/agents/{id}/install-command. The control-plane serves it directly so
// `curl <panel>/install-agent.sh | sudo bash -s -- ...` works without a
// dependency on an external CDN — the panel is its own distribution channel.
//
//go:embed install_agent.sh
var installAgentScript []byte

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
func (s *Server) handleInstallAgentScript() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		h := w.Header()
		h.Set("Content-Type", "text/x-shellscript; charset=utf-8")
		h.Set("Content-Length", strconv.Itoa(len(installAgentScript)))
		// Stable script body — operators may pin a specific panel version
		// for reproducible installs. Cache for 5 minutes; release of a new
		// panel version invalidates downstream caches via the response body
		// hash that operators pin in CI.
		h.Set("Cache-Control", "public, max-age=300")
		_, _ = w.Write(installAgentScript)
	}
}
