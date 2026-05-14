import {
  DEFAULT_TELEMT_METRICS_URL,
  DEFAULT_TELEMT_URL,
  GITHUB_REPO,
} from "@/shared/lib/defaults";
import { shellQuote } from "@/shared/lib/shell-quote";

export interface InstallCommandAdvancedOptions {
  telemtUrl: string;
  telemtMetricsUrl: string;
  telemtAuth: string;
  insecureTransport: boolean;
}

// scriptUrl defaults to the upstream GitHub-raw URL so existing callers
// keep working before PR-3b parametrises the wizard's source toggle.
// Pre-PR-2a the URL was inlined here; now it is a parameter callers
// fetch from `script_sources` on the enrollment-token response (panel:
// hashed; github: legacy).
const DEFAULT_GITHUB_SCRIPT_URL = `https://raw.githubusercontent.com/${GITHUB_REPO}/main/deploy/install-agent.sh`;

// Build the one-liner the operator pastes into a root shell on a fresh
// box. Every interpolated value is shell-quoted (POSIX single-quote
// idiom) so a malicious or merely mistyped value cannot escape its
// argv slot and chain a second command. The caller is responsible for
// validating `nodeName` against isValidNodeName before reaching this
// function — the quoting here protects against accidental breakage,
// the whitelist protects the UX (no surprising commands rendered).
//
// `scriptUrl` controls which copy of install-agent.sh the curl points
// at. Pass `script_sources.panel.url` (with sha256) or
// `script_sources.github.url` (sha256=null) from the API response. When
// omitted, defaults to the upstream GitHub-raw URL (back-compat for
// callers not yet threaded through the source toggle).
//
// `scriptSha256` — when set, renders the temp-file + sha256sum
// verification form so the operator's shell verifies the body before
// `sudo bash` runs it (the same form `bootstrap.BuildInstallCommand`
// emits server-side). When omitted/null, falls back to the legacy
// `curl | sudo bash -s --` form (correct for the GitHub source, since
// the panel cannot vouch for upstream bytes).
export function buildInstallCommand(
  panelUrl: string,
  tokenValue: string,
  nodeName: string,
  advancedOptions?: InstallCommandAdvancedOptions,
  scriptUrl: string = DEFAULT_GITHUB_SCRIPT_URL,
  scriptSha256: string | null = null,
): string {
  const flags = buildInstallFlags(panelUrl, tokenValue, nodeName, advancedOptions);
  if (scriptSha256) {
    return buildVerifiedForm(scriptUrl, scriptSha256, flags);
  }
  return buildLegacyForm(scriptUrl, flags);
}

// buildInstallFlags renders the trailing `\` -separated CLI flags both
// forms of the command pipe to bash. Extracted so the verified and
// legacy curl forms share quoting verbatim.
function buildInstallFlags(
  panelUrl: string,
  tokenValue: string,
  nodeName: string,
  advancedOptions?: InstallCommandAdvancedOptions,
): string {
  let flags =
    `    --panel-url ${shellQuote(panelUrl)} \\\n` +
    `    --token ${shellQuote(tokenValue)} \\\n` +
    `    --node-name ${shellQuote(nodeName)}`;

  if (advancedOptions?.telemtUrl && advancedOptions.telemtUrl !== DEFAULT_TELEMT_URL) {
    flags += ` \\\n    --telemt-url ${shellQuote(advancedOptions.telemtUrl)}`;
  }
  // Metrics URL is a first-class knob in the wizard because Telemt
  // ships with metrics off. Only append the flag when the operator
  // changed it from the agent's built-in default.
  if (
    advancedOptions?.telemtMetricsUrl &&
    advancedOptions.telemtMetricsUrl !== DEFAULT_TELEMT_METRICS_URL
  ) {
    flags += ` \\\n    --telemt-metrics-url ${shellQuote(advancedOptions.telemtMetricsUrl)}`;
  }
  if (advancedOptions?.telemtAuth) {
    flags += ` \\\n    --telemt-auth ${shellQuote(advancedOptions.telemtAuth)}`;
  }
  // Explicit opt-in: the agent otherwise rejects plain-HTTP panel URLs
  // outside loopback. For VPN-only / private-network panels this flag
  // relaxes the guard — the operator acknowledges the bootstrap private
  // key transits in cleartext.
  if (advancedOptions?.insecureTransport) {
    flags += ` \\\n    --insecure-transport`;
  }
  return flags;
}

// buildLegacyForm renders `curl ... | sudo bash -s -- <flags>`. Used
// for the GitHub source (sha256 nil) since the panel cannot vouch for
// upstream bytes and there is nothing to verify against.
function buildLegacyForm(scriptUrl: string, flags: string): string {
  return (
    `curl -fsSL ${scriptUrl} | \\\n` +
    `  sudo bash -s -- \\\n` +
    flags
  );
}

// buildVerifiedForm renders the temp-file + sha256sum verification
// form. Matches the shape `bootstrap.BuildInstallCommand` emits when
// `ScriptHash` is non-empty so an operator who pastes either command
// gets the same MITM-resistant download path.
function buildVerifiedForm(scriptUrl: string, scriptSha256: string, flags: string): string {
  return (
    `TMP=$(mktemp /tmp/panvex-install.XXXXXX) || exit 1\n` +
    `trap 'rm -f "$TMP"' EXIT\n` +
    `curl -fsSL ${scriptUrl} -o "$TMP" || { echo 'panvex: install-script download failed' >&2; exit 1; }\n` +
    `ACTUAL=$(sha256sum < "$TMP" | awk '{print $1}')\n` +
    `if [ "$ACTUAL" != ${shellQuote(scriptSha256)} ]; then\n` +
    `  echo "panvex: install-script hash mismatch (expected ${scriptSha256}, got $ACTUAL)" >&2\n` +
    `  exit 1\n` +
    `fi\n` +
    `sudo -E PANVEX_INSTALL_SCRIPT_SHA256=${shellQuote(scriptSha256)} bash "$TMP" -- \\\n` +
    flags
  );
}
