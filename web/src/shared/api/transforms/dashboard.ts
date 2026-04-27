import type {
  DashboardOverviewData,
  DashboardTimelineData,
  DashboardNodeData,
} from "@/ui";
import type {
  TelemetryDashboardResponse,
  TelemetryAttentionItem,
  TelemetryAgentLoadSeries,
  TelemetryRecentEvent,
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

function mapAttentionItemToNode(
  item: TelemetryAttentionItem,
  seriesByAgent: Map<string, TelemetryAgentLoadSeries>,
): DashboardNodeData {
  const runtime = item.runtime;
  const series = seriesByAgent.get(item.agent_id);
  return {
    id: item.agent_id,
    name: item.node_name,
    status: mapSeverity(item.severity),
    connections: runtime?.current_connections ?? 0,
    trafficBytes: 0,
    cpuPct: pct1(runtime?.system_load?.cpu_usage_pct),
    memPct: pct1(runtime?.system_load?.memory_usage_pct),
    dcs: mapDcs(runtime?.dcs ?? []),
    cpuSeries: series?.cpu_pct,
    memSeries: series?.mem_pct,
  };
}

function formatNumber(n: number): string {
  return n.toLocaleString();
}

interface AgentRuntimeSnapshot {
  cpu: number;
  mem: number;
  dcCoverage: number;
}

// Aggregate runtime stats from both attention items (problem nodes carry full
// runtime) and server_cards (healthy nodes also have runtime nested under
// agent.runtime). De-dupe by agent id in case the backend lists the same
// node in both arrays.
function buildRuntimeIndex(
  raw: TelemetryDashboardResponse,
): Map<string, AgentRuntimeSnapshot> {
  const out = new Map<string, AgentRuntimeSnapshot>();
  for (const item of raw.attention ?? []) {
    const r = item.runtime;
    if (!r) continue;
    out.set(item.agent_id, {
      cpu: pct1(r.system_load?.cpu_usage_pct),
      mem: pct1(r.system_load?.memory_usage_pct),
      dcCoverage: pct1(r.dc_coverage_pct),
    });
  }
  for (const card of raw.server_cards ?? []) {
    const id = card.agent?.id;
    if (!id || out.has(id)) continue;
    const r = card.agent?.runtime;
    if (!r) continue;
    out.set(id, {
      cpu: pct1(r.system_load?.cpu_usage_pct),
      mem: pct1(r.system_load?.memory_usage_pct),
      dcCoverage: pct1(r.dc_coverage_pct),
    });
  }
  return out;
}

type KpiTone = "ok" | "warn" | "error" | "default";

function fleetHealthSub(
  offline: number,
  degraded: number,
): string {
  if (offline > 0) return `${offline} offline · ${degraded} degraded`;
  if (degraded > 0) return `${degraded} degraded`;
  return "all online";
}

export function transformDashboardOverview(
  raw: TelemetryDashboardResponse
): DashboardOverviewData {
  const fleet = raw.fleet;
  const runtimes = Array.from(buildRuntimeIndex(raw).values());
  const avg = (xs: number[]) =>
    xs.length ? Math.round(xs.reduce((a, b) => a + b, 0) / xs.length) : 0;
  const avgCpu = avg(runtimes.map((r) => r.cpu));
  const avgMem = avg(runtimes.map((r) => r.mem));
  const avgDcCoverage = avg(runtimes.map((r) => r.dcCoverage));

  // Tone drives the value color. Fleet health goes warn/error only when nodes
  // are actually offline or degraded — a healthy fleet stays neutral so the
  // color signal is preserved for real issues.
  const fleetTone: KpiTone = (() => {
    if (fleet.offline_agents > 0) return "error";
    if (fleet.degraded_agents > 0) return "warn";
    return "ok";
  })();
  const cpuTone: KpiTone = (() => {
    if (avgCpu >= 90) return "error";
    if (avgCpu >= 70) return "warn";
    return "default";
  })();
  const coverageTone: KpiTone = (() => {
    if (avgDcCoverage < 95) return "error";
    if (avgDcCoverage < 100) return "warn";
    return "ok";
  })();

  const kpis = [
    {
      label: "Fleet health",
      value: `${fleet.online_agents}/${fleet.total_agents}`,
      sub: fleetHealthSub(fleet.offline_agents, fleet.degraded_agents),
      tone: fleetTone,
    },
    {
      label: "Connections",
      value: formatNumber(fleet.live_connections),
      sub: "active sessions",
    },
    {
      label: "Avg CPU · Mem",
      value: `${avgCpu}% · ${avgMem}%`,
      sub: avgCpu >= 70 || avgMem >= 70 ? "resource pressure" : "within limits",
      tone: cpuTone,
    },
    {
      label: "DC coverage",
      value: `${avgDcCoverage}%`,
      sub: `${fleet.dc_issue_agents} agent${fleet.dc_issue_agents === 1 ? "" : "s"} with DC issues`,
      tone: coverageTone,
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

  const seriesByAgent = new Map<string, TelemetryAgentLoadSeries>(
    (raw.agent_load_series ?? []).map((s) => [s.agent_id, s]),
  );

  const attentionNodes = (raw.attention ?? [])
    .filter((item) => item.severity !== "good")
    .map((item) => mapAttentionItemToNode(item, seriesByAgent));

  const healthyNodes = (raw.server_cards ?? [])
    .filter((card) => card.severity === "good")
    .map((card) => {
      const runtime = card.agent?.runtime;
      const series = seriesByAgent.get(card.agent?.id ?? "");
      return {
        id: card.agent?.id ?? "",
        name: card.agent?.node_name ?? "",
        status: "ok" as const,
        connections: runtime?.current_connections ?? 0,
        trafficBytes: 0,
        cpuPct: pct1(runtime?.system_load?.cpu_usage_pct),
        memPct: pct1(runtime?.system_load?.memory_usage_pct),
        dcs: mapDcs(runtime?.dcs ?? []),
        cpuSeries: series?.cpu_pct,
        memSeries: series?.mem_pct,
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

/**
 * Render the backend's raw event shape into a single human-readable line:
 *   "admission.state" + "accepting_new_connections=true"
 *     -> "Accepting new connections"
 *   "me.runtime" + "ready=true"
 *     -> "ME runtime ready"
 *   "dc.coverage" + "dc=3,pct=62"
 *     -> "DC3 coverage dropped to 62%"
 *
 * Unknown event types fall back to a Title-cased version of the event type
 * with the context trimmed to a sensible length, so new backend events are
 * still legible without a frontend change.
 */
function formatRuntimeEvent(eventType: string, context: string): string {
  const ctx = context.trim();
  const kv = Object.fromEntries(
    ctx
      .split(",")
      .map((pair) => pair.trim().split("="))
      .filter((parts) => parts.length === 2) as Array<[string, string]>,
  );

  if (eventType === "admission.state") {
    const on = kv.accepting_new_connections === "true";
    return on ? "Accepting new connections" : "Stopped accepting new connections";
  }
  if (eventType === "me.runtime") {
    return kv.ready === "true" ? "ME runtime ready" : "ME runtime not ready";
  }
  if (eventType === "dc.coverage" && kv.dc && kv.pct) {
    return `DC${kv.dc} coverage ${kv.pct}%`;
  }
  if (eventType === "reroute" && kv.active) {
    return kv.active === "true" ? "Reroute activated" : "Reroute cleared";
  }
  if (eventType === "gateway.stream") {
    return kv.state ? `Gateway stream ${kv.state}` : "Gateway stream event";
  }

  // Fallback: sentence-case event type + trimmed context as a suffix when it
  // adds signal. Caps at 80 chars to protect the timeline column width.
  const humanized = eventType
    ? eventType.replaceAll(/[._-]+/g, " ").replaceAll(/\b\w/g, (c) => c.toUpperCase())
    : "Unknown event";
  const body = ctx && ctx !== eventType ? `${humanized} · ${ctx}` : humanized;
  return body.length > 80 ? `${body.slice(0, 77)}…` : body;
}

export function transformDashboardTimeline(
  raw: TelemetryDashboardResponse
): DashboardTimelineData {
  // Prefer the enriched feed (has agent info so the UI can show
  // "node-name · message"). Falls back to the legacy untagged feed for
  // backward-compatibility with older control-plane builds.
  const enriched = raw.recent_events ?? [];
  if (enriched.length > 0) {
    const events = [...enriched]
      .sort(
        (a: TelemetryRecentEvent, b: TelemetryRecentEvent) =>
          b.timestamp_unix - a.timestamp_unix || b.sequence - a.sequence,
      )
      .map((event) => ({
        status: mapEventSeverity(event.event_type ?? ""),
        time: formatEventTime(event.timestamp_unix),
        message: formatRuntimeEvent(event.event_type ?? "", event.context ?? ""),
        source: event.node_name || undefined,
      }));
    return { events };
  }

  const events = [...(raw.recent_runtime_events ?? [])]
    .sort(
      (a: RuntimeEvent, b: RuntimeEvent) =>
        b.timestamp_unix - a.timestamp_unix || b.sequence - a.sequence
    )
    .map((event: RuntimeEvent) => ({
      status: mapEventSeverity(event.event_type ?? ""),
      time: formatEventTime(event.timestamp_unix),
      message: formatRuntimeEvent(event.event_type ?? "", event.context ?? ""),
    }));

  return { events };
}
