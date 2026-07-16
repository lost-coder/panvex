package bootstrap

import (
	"fmt"
	"strings"
	"time"
)

// shellQuote wraps s in single quotes, escaping any embedded single quote
// as '\” — the standard POSIX-safe form. Used so operator-controlled
// values (panel/script URLs) embedded in the install one-liner cannot
// break out of the command.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// InstallCommandTTL bounds how long an issued bootstrap token is valid.
// 5 minutes is the S-02 upper-bound: an operator copies the curl one-liner
// and runs it immediately; a leaked token is only exploitable for a very short
// window before it expires.  Changing this constant above 5 minutes would
// violate the S-02 security requirement — see the regression test
// TestBootstrapToken_DefaultTTLIsAtMost5Minutes.
const InstallCommandTTL = 5 * time.Minute

// InstallCommandInput carries the fields BuildInstallCommand interpolates into
// the curl|bash one-liner. (S-3.)
type InstallCommandInput struct {
	ScriptURL  string
	ScriptHash string // lowercase hex SHA-256; "" disables verification (legacy)
	Token      string
	AgentID    string
	ListenAddr string
	PanelCAPin string
	PanelCN    string
	PanelURL   string
}

// BuildInstallCommand returns the shell one-liner the operator runs on the
// agent host. When ScriptHash is non-empty the command:
//
//  1. Allocates a temp file via mktemp and registers a trap to delete it on
//     exit. We deliberately avoid the `SCRIPT=$(curl ...)` round-trip: POSIX
//     `$()` strips trailing newlines, so hashing the captured variable would
//     diverge from the byte-exact server hash (which covers the embedded
//     file as-is, including its trailing \n) and every install would fail
//     with a hash mismatch.
//  2. Downloads the script to the temp file with curl -o.
//  3. Hashes the file with `sha256sum < file` and compares against the
//     expected digest. On mismatch the command exits non-zero before any
//     privileged execution.
//  4. Execs the file via `sudo -E bash <file> -- <flags>` with
//     PANVEX_INSTALL_SCRIPT_SHA256 exported so the script's own self-check
//     (T-5) re-validates against the same digest in the privileged context.
//
// When ScriptHash is empty (test fixtures, transitional configs that have
// not been re-deployed yet) the legacy curl|bash form is emitted unchanged
// so the install path keeps working — at the cost of MITM exposure that
// the deploy is opting into. (S-3.)
func BuildInstallCommand(in InstallCommandInput) string {
	// Every interpolated value is shell-single-quoted: Token, AgentID,
	// ListenAddr, PanelCAPin, PanelCN and PanelURL are (now) operator- or
	// request-controlled, so an unquoted %s would let a crafted value
	// (e.g. a malicious grpc.public_endpoint / http.public_url) break out
	// of the command an operator pastes into a root shell. The `=` stays
	// outside the quotes so `--flag='value'` remains a single argv token.
	flags := fmt.Sprintf(
		"--mode=reverse --bootstrap-token=%s --agent-id=%s --listen-addr=%s --ca-pin=%s --panel-cn=%s --panel-url-grpc=%s",
		shellQuote(in.Token), shellQuote(in.AgentID), shellQuote(in.ListenAddr),
		shellQuote(in.PanelCAPin), shellQuote(in.PanelCN), shellQuote(in.PanelURL),
	)
	if in.ScriptHash == "" {
		return fmt.Sprintf("curl -fsSL %s | sudo bash -s -- %s", shellQuote(in.ScriptURL), flags)
	}
	// The single-line form is intentional — operators copy the whole thing
	// from the dashboard and paste it into a shell. Keeping it on one line
	// also means a copy that drops trailing newlines does not split the
	// pipeline. We hash the file on disk (`sha256sum < "$TMP"`) so the
	// digest is byte-exact with what the panel hashes server-side; piping
	// `$()`-captured bytes would silently strip trailing newlines.
	return fmt.Sprintf(
		`TMP=$(mktemp /tmp/panvex-install.XXXXXX) || exit 1; trap 'rm -f "$TMP"' EXIT; curl -fsSL %s -o "$TMP" || { echo 'panvex: install-script download failed' >&2; exit 1; }; ACTUAL=$(sha256sum < "$TMP" | awk '{print $1}'); if [ "$ACTUAL" != "%s" ]; then echo "panvex: install-script hash mismatch (expected %s, got $ACTUAL)" >&2; exit 1; fi; sudo -E PANVEX_INSTALL_SCRIPT_SHA256="%s" bash "$TMP" %s`,
		shellQuote(in.ScriptURL), in.ScriptHash, in.ScriptHash, in.ScriptHash, flags,
	)
}
