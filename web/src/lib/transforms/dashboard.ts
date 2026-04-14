import type {
  DashboardOverviewData,
  DashboardTimelineData,
  DashboardNodeData,
} from "@lost-coder/panvex-ui";
import type {
  TelemetryDashboardResponse,
  TelemetryAttentionItem,
  RuntimeEvent,
} from "../api";

function pct1(v: number | undefined | null): number {
  return v ? Math.round((v ?? 0) * 10) / 10 : 0;
}

function load2(v: number | undefined | null): number {
  return v ? Math.round((v ?? 0) * 100) / 100 : 0;
}

// Map API severity to UI Severity type
function mapSeverity(s: "good" | "warn" | "bad"): "ok" | "warn" | "error" {
  if (s === "good") return "ok";
  if (s === "bad") return "error";
  return "warn";
}

// Map API dc entries to UI NodeDcInfo shape
function mapDcs(
  dcs: Array<{
    dc: number;
    coverage_pct: number;
    rtt_ms: number;
    load: number;
  }>
): DashboardNodeData["dcs"] {
  return dcs.map((dc) => ({
    dc: dc.dc,
    status:
      dc.coverage_pct >= 99.5 ? "ok" : dc.coverage_pct > 0 ? "warn" : "error",
    rttMs: dc.rtt_ms > 0 ? Math.round(dc.rtt_ms * 10) / 10 : null,
    coveragePct: pct1(dc.coverage_pct),
    load: load2(dc.load),
  }));
}

function mapAttentionItemToNode(item: TelemetryAttentionItem): DashboardNodeData {
  const runtime = item.runtime;
  return {
    id: item.agent_id,
    name: item.node_name,
    status: mapSeverity(item.severity),
    connections: runtime?.current_connections ?? 0,
    trafficBytes: 0,
    cpuPct: pct1(runtime?.system_load?.cpu_usage_pct),
    memPct: pct1(runtime?.system_load?.memory_usage_pct),
    dcs: mapDcs(runtime?.dcs ?? []),
  };
}

export function transformDashboardOverview(
  raw: TelemetryDashboardResponse
): DashboardOverviewData {
  const fleet = raw.fleet;

  const kpis = [
    {
      label: "Total Servers",
      value: String(fleet.total_agents),
      sub: `${fleet.online_agents} online`,
    },
    {
      label: "Online",
      value: String(fleet.online_agents),
      sub: "Agents reachable",
      accent: fleet.online_agents === fleet.total_agents,
    },
    {
      label: "Degraded",
      value: String(fleet.degraded_agents),
      sub: "Agents with issues",
      accent: fleet.degraded_agents > 0,
    },
    {
      label: "Offline",
      value: String(fleet.offline_agents),
      sub: "Agents unreachable",
      accent: fleet.offline_agents > 0,
    },
    {
      label: "Live Connections",
      value: String(fleet.live_connections),
      sub: "Active sessions",
    },
    {
      label: "Instances",
      value: String(fleet.total_instances),
      sub: "Running instances",
    },
  ];

  const trends = Object.entries(raw.runtime_distribution ?? {}).map(
    ([label, count]) => ({
      label,
      data: [count],
      color: "var(--color-accent)",
      current: String(count),
    })
  );

  const alerts = (raw.attention ?? [])
    .filter((item) => item.severity !== "good")
    .map((item) => ({
      severity:
        item.severity === "bad" ? ("crit" as const) : ("warn" as const),
      message: item.reason,
      source: item.node_name,
      timestamp: new Date(
        (item.runtime_freshness?.observed_at_unix ?? 0) * 1000
      ).toISOString(),
    }));

  const attentionNodes = (raw.attention ?? [])
    .filter((item) => item.severity !== "good")
    .map(mapAttentionItemToNode);

  const healthyNodes = (raw.server_cards ?? [])
    .filter((card) => card.severity === "good")
    .map((card) => {
      const runtime = card.agent?.runtime;
      return {
        id: card.agent?.id ?? "",
        name: card.agent?.node_name ?? "",
        status: "ok" as const,
        connections: runtime?.current_connections ?? 0,
        trafficBytes: 0,
        cpuPct: pct1(runtime?.system_load?.cpu_usage_pct),
        memPct: pct1(runtime?.system_load?.memory_usage_pct),
        dcs: mapDcs(runtime?.dcs ?? []),
      };
    });

  return { kpis, trends, alerts, attentionNodes, healthyNodes };
}

/** Extract a map of node id -> agent version from dashboard server cards. */
export function extractDashboardAgentVersions(
  raw: TelemetryDashboardResponse
): Record<string, string> {
  const map: Record<string, string> = {};
  for (const card of raw.server_cards ?? []) {
    if (card.agent?.id && card.agent.version) {
      map[card.agent.id] = card.agent.version;
    }
  }
  return map;
}

function mapEventSeverity(
  eventType: string
): "ok" | "warn" | "error" | "info" {
  const t = eventType.toLowerCase();
  if (
    t.includes("error") ||
    t.includes("fail") ||
    t.includes("disconnect") ||
    t.includes("offline") ||
    t.includes("crash") ||
    t.includes("down")
  ) {
    return "error";
  }
  if (
    t.includes("warn") ||
    t.includes("timeout") ||
    t.includes("retry") ||
    t.includes("slow") ||
    t.includes("degrad")
  ) {
    return "warn";
  }
  if (
    t.includes("connect") ||
    t.includes("online") ||
    t.includes("ready") ||
    t.includes("recover")
  ) {
    return "ok";
  }
  return "info";
}

function formatEventTime(tsUnix: number): string {
  if (!tsUnix || !Number.isFinite(tsUnix)) return "unknown";
  const diffMs = Math.max(0, Date.now() - tsUnix * 1000);
  const mins = Math.floor(diffMs / 60_000);
  if (mins < 1) return "just now";
  if (mins < 60) return `${mins} min ago`;
  const hrs = Math.floor(mins / 60);
  if (hrs < 24) return `${hrs} hr ago`;
  return `${Math.floor(hrs / 24)} d ago`;
}

export function transformDashboardTimeline(
  raw: TelemetryDashboardResponse
): DashboardTimelineData {
  const events = [...(raw.recent_runtime_events ?? [])]
    .sort(
      (a: RuntimeEvent, b: RuntimeEvent) =>
        b.timestamp_unix - a.timestamp_unix || b.sequence - a.sequence
    )
    .map((event: RuntimeEvent) => ({
      status: mapEventSeverity(event.event_type ?? ""),
      time: formatEventTime(event.timestamp_unix),
      message: event.context || event.event_type || "Unknown event",
    }));

  return { events };
}
