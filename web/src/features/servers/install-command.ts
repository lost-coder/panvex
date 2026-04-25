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

// Build the one-liner the operator pastes into a root shell on a fresh
// box. Every interpolated value is shell-quoted (POSIX single-quote
// idiom) so a malicious or merely mistyped value cannot escape its
// argv slot and chain a second command. The caller is responsible for
// validating `nodeName` against isValidNodeName before reaching this
// function — the quoting here protects against accidental breakage,
// the whitelist protects the UX (no surprising commands rendered).
export function buildInstallCommand(
  panelUrl: string,
  tokenValue: string,
  nodeName: string,
  advancedOptions?: InstallCommandAdvancedOptions,
): string {
  let cmd =
    `curl -fsSL https://raw.githubusercontent.com/${GITHUB_REPO}/main/deploy/install-agent.sh | \\\n` +
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
