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

// Default install-script URL when the caller doesn't pick a source.
// Backwards compatible — pre-PR-3a callers (and the existing wizard
// tests) get the upstream GitHub-raw copy.
const DEFAULT_GITHUB_SCRIPT_URL = `https://raw.githubusercontent.com/${GITHUB_REPO}/main/deploy/install-agent.sh`;

// Build the one-liner the operator pastes into a root shell. Every
// interpolated value is shell-quoted (POSIX single-quote idiom) so a
// malicious or merely mistyped value cannot escape its argv slot and
// chain a second command. The caller is responsible for validating
// `nodeName` against isValidNodeName before reaching this function —
// the quoting protects against accidental breakage, the whitelist
// protects the UX (no surprising commands rendered).
//
// `scriptUrl` controls which copy of install-agent.sh the curl points
// at: `script_sources.panel.url` (panel-served) or
// `script_sources.github.url` (legacy raw). The output is always the
// simple `curl URL | sudo bash -s -- <flags>` form — that's the form
// operators recognise at a glance and the form the install script is
// designed to consume. SHA-256 self-verification (PANVEX_INSTALL_SCRIPT_SHA256)
// is available to operators who want it, but rendering it inline turned
// the wizard's "paste this" affordance into a 7-line shell snippet, so
// the wizard keeps the simple form and surfaces the digest separately
// for manual verification (handled by the rendering component, not
// this builder).
export function buildInstallCommand(
  panelUrl: string,
  tokenValue: string,
  nodeName: string,
  advancedOptions?: InstallCommandAdvancedOptions,
  scriptUrl: string = DEFAULT_GITHUB_SCRIPT_URL,
): string {
  let cmd =
    `curl -fsSL ${scriptUrl} | \\\n` +
    `  sudo bash -s -- \\\n` +
    `    --panel-url ${shellQuote(panelUrl)} \\\n` +
    `    --token ${shellQuote(tokenValue)} \\\n` +
    `    --node-name ${shellQuote(nodeName)}`;

  if (advancedOptions?.telemtUrl && advancedOptions.telemtUrl !== DEFAULT_TELEMT_URL) {
    cmd += ` \\\n    --telemt-url ${shellQuote(advancedOptions.telemtUrl)}`;
  }
  // Metrics URL is a first-class knob in the wizard because Telemt
  // ships with metrics off. Only append the flag when the operator
  // changed it from the agent's built-in default.
  if (
    advancedOptions?.telemtMetricsUrl &&
    advancedOptions.telemtMetricsUrl !== DEFAULT_TELEMT_METRICS_URL
  ) {
    cmd += ` \\\n    --telemt-metrics-url ${shellQuote(advancedOptions.telemtMetricsUrl)}`;
  }
  if (advancedOptions?.telemtAuth) {
    cmd += ` \\\n    --telemt-auth ${shellQuote(advancedOptions.telemtAuth)}`;
  }
  // Explicit opt-in: the agent otherwise rejects plain-HTTP panel URLs
  // outside loopback. For VPN-only / private-network panels this flag
  // relaxes the guard — the operator acknowledges the bootstrap private
  // key transits in cleartext.
  if (advancedOptions?.insecureTransport) {
    cmd += ` \\\n    --insecure-transport`;
  }
  return cmd;
}
