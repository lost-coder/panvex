export interface NamespaceLabel {
  title: string;
  desc: string;
}

export const namespaceLabels: Record<string, NamespaceLabel> = {
  http: {
    title: "Public access",
    desc: "Public URLs the panel advertises to agents and clients.",
  },
  agents: {
    title: "Agents",
    desc: "Heartbeat thresholds and outbound supervisor backoff.",
  },
  auth: {
    title: "Authentication",
    desc: "Password policy, session timeouts, TOTP windows.",
  },
  jobs: {
    title: "Jobs",
    desc: "Job worker cadences and TTLs.",
  },
  observability: {
    title: "Observability",
    desc: "Telemetry sampling and dashboard windows.",
  },
  storage: {
    title: "Storage",
    desc: "Batch flush + rollup cadences.",
  },
};

export function namespaceOf(name: string): string {
  const dot = name.indexOf(".");
  return dot >= 0 ? name.slice(0, dot) : name;
}

export function labelFor(namespace: string): NamespaceLabel {
  return namespaceLabels[namespace] ?? { title: namespace, desc: "" };
}
