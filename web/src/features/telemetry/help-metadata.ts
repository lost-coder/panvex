export type TelemetryHelpMode = "off" | "basic" | "full";

const fieldHelp: Record<string, string> = {
  "Health": "Primary operator diagnosis for why the node needs attention right now.",
  "Freshness": "Telemetry freshness shows whether the latest runtime summary is still current enough for triage.",
  "Runtime": "Current transport mode and live connection load reported by the node.",
  "DC Health": "Aggregate Telegram data center coverage and health summary for the node.",
  "Upstreams": "Outbound route health summary for the node's current upstream set.",
  "Events": "Latest runtime signal reported by the node to explain recent state changes.",
  "Config Hash": "Current config fingerprint used to compare whether nodes run the same Telemt configuration.",
  "Config Reloads": "Number of successful runtime config reloads observed since the process started.",
  "Update Every": "Effective global runtime update interval used by Telemt internal refresh loops.",
  "ME Reinit": "Interval for periodic ME runtime reinitialization.",
  "Force Close": "Timeout after which stale ME writers are force-closed.",
  "IP Policy": "Effective unique-IP policy mode applied to managed users.",
  "IP Window": "Time window used by the unique-IP accounting policy.",
  "Floor Mode": "How Telemt decides the minimum ME writer floor for each DC.",
  "Writer Pick Mode": "Strategy Telemt uses when choosing a writer from the available ME pool.",
  "API Read-Only": "When enabled, mutating Telemt API routes are rejected.",
  "Whitelist Enabled": "Whether API access is limited to configured CIDR allowlist entries.",
  "Whitelist Entries": "Count of configured allowlist CIDR entries currently active for the Telemt API.",
  "Auth Header": "Whether Telemt currently requires the Authorization header for API access.",
  "Proxy Protocol": "Whether Telemt accepts inbound PROXY protocol metadata on listener sockets.",
  "Telemetry Core": "Core runtime telemetry switch inside Telemt.",
  "Telemetry User": "Per-user telemetry switch inside Telemt.",
  "Telemetry ME": "Verbosity level for ME-specific telemetry.",
  "Active Generation": "Currently active ME pool generation serving requests.",
  "Warm Generation": "Prepared ME pool generation waiting to replace the active one.",
  "Pending Hardswap": "Generation queued for hard swap when Telemt decides the current active pool must be replaced.",
  "Boost": "Detail boost temporarily raises diagnostics refresh priority for one node while the operator is investigating it.",
};

export function getTelemetryFieldHelp(label: string): string | undefined {
  return fieldHelp[label];
}
