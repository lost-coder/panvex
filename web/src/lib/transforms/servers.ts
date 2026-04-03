import type { ServerListItem } from "@panvex/ui";
import type { ServerDetailPageProps, InitCardProps } from "@panvex/ui";
import type {
  TelemetryServersResponse,
  TelemetryServerDetailResponse,
  TelemetryServerSummary,
  Agent,
} from "../api";

type Severity = "ok" | "warn" | "error";

function mapSeverity(s: "good" | "warn" | "bad"): Severity {
  if (s === "good") return "ok";
  if (s === "bad") return "error";
  return "warn";
}

function resolveAgentSeverity(agent: Agent): Severity {
  if (agent.presence_state === "offline") return "error";
  if (agent.presence_state === "degraded" || agent.runtime?.degraded)
    return "warn";
  return "ok";
}

function mapDcs(
  dcs: Agent["runtime"]["dcs"]
): Array<{ dc: number; status: Severity; rttMs: number | null; coveragePct?: number; load?: number }> {
  return (dcs ?? []).map((dc) => ({
    dc: dc.dc,
    status:
      dc.coverage_pct >= 99.5 ? "ok" : dc.coverage_pct > 0 ? "warn" : "error",
    rttMs: dc.rtt_ms > 0 ? dc.rtt_ms : null,
    coveragePct: dc.coverage_pct,
    load: dc.load,
  }));
}

function summaryToListItem(card: TelemetryServerSummary): ServerListItem {
  const agent = card.agent;
  const runtime = agent?.runtime;
  return {
    id: agent?.id ?? "",
    name: agent?.node_name ?? "",
    status: mapSeverity(card.severity),
    connections: runtime?.current_connections ?? 0,
    trafficBytes: 0,
    cpuPct: 0,
    memPct: 0,
    dcCoveragePct: runtime?.dc_coverage_pct ?? 0,
    uptimeSeconds: runtime?.uptime_seconds ?? 0,
    fleetGroupId: agent?.fleet_group_id ?? "",
    dcs: mapDcs(runtime?.dcs ?? []),
  };
}

export function transformServerList(
  raw: TelemetryServersResponse
): ServerListItem[] {
  return (raw.servers ?? []).map(summaryToListItem);
}

export function transformInitState(
  raw: TelemetryServerDetailResponse
): InitCardProps | undefined {
  const iw = raw.initialization_watch;
  if (!iw?.visible || iw.mode !== "active") return undefined;

  const runtime = raw.server?.agent?.runtime;
  return {
    stage: iw.initialization_stage || iw.startup_stage || "initializing",
    progressPct: iw.initialization_progress_pct ?? iw.startup_progress_pct ?? 0,
    attempt: 1,
    retryLimit: 1,
    elapsedSecs: iw.completed_at_unix > 0
      ? 0
      : iw.remaining_seconds > 0
        ? iw.remaining_seconds
        : 0,
    degraded: runtime?.degraded ?? false,
  };
}

export function transformServerDetail(
  raw: TelemetryServerDetailResponse
): ServerDetailPageProps["server"] {
  const agent = raw.server?.agent;
  const runtime = agent?.runtime;
  const detail = raw;

  const gates: ServerDetailPageProps["server"]["gates"] = {
    acceptingNewConnections: runtime?.accepting_new_connections ?? false,
    meRuntimeReady: runtime?.me_runtime_ready ?? false,
    useMiddleProxy: runtime?.use_middle_proxy ?? false,
    me2dcFallbackEnabled: runtime?.me2dc_fallback_enabled ?? false,
    rerouteActive: false,
    startupStatus: runtime?.startup_status ?? "",
    startupProgressPct: runtime?.startup_progress_pct ?? 0,
    degraded: runtime?.degraded ?? false,
    readOnly: agent?.read_only ?? false,
  };

  const dcs: ServerDetailPageProps["server"]["dcs"] = (
    runtime?.dcs ?? []
  ).map((dc) => ({
    dc: dc.dc,
    endpoints: [],
    endpointWriters: [],
    availableEndpoints: dc.available_endpoints ?? 0,
    availablePct: dc.available_pct ?? 0,
    requiredWriters: dc.required_writers ?? 0,
    aliveWriters: dc.alive_writers ?? 0,
    coveragePct: dc.coverage_pct ?? 0,
    floorMin: 0,
    floorTarget: 0,
    floorMax: 0,
    floorCapped: false,
    rttMs: dc.rtt_ms > 0 ? dc.rtt_ms : undefined,
    load: dc.load ?? 0,
  }));

  const connections: ServerDetailPageProps["server"]["connections"] = {
    current: runtime?.current_connections ?? 0,
    currentMe: runtime?.current_connections_me ?? 0,
    currentDirect: runtime?.current_connections_direct ?? 0,
    activeUsers: runtime?.active_users ?? 0,
    staleCacheUsed: false,
    topByConnections: [],
    topByThroughput: [],
  };

  const summary: ServerDetailPageProps["server"]["summary"] = {
    connectionsTotal: runtime?.connections_total ?? 0,
    connectionsBadTotal: runtime?.connections_bad_total ?? 0,
    handshakeTimeoutsTotal: runtime?.handshake_timeouts_total ?? 0,
    configuredUsers: runtime?.configured_users ?? 0,
  };

  const upstreams: ServerDetailPageProps["server"]["upstreams"] = (
    runtime?.upstreams ?? []
  ).map((u) => ({
    upstreamId: u.upstream_id,
    routeKind: u.route_kind ?? "direct",
    address: u.address ?? "",
    weight: 1,
    healthy: u.healthy ?? false,
    fails: u.fails ?? 0,
    lastCheckAgeSecs: 0,
    effectiveLatencyMs:
      (u.effective_latency_ms ?? 0) > 0 ? u.effective_latency_ms : undefined,
    dc: [],
  }));

  const events: ServerDetailPageProps["server"]["events"] = [
    ...(runtime?.recent_events ?? []),
  ]
    .sort(
      (a, b) =>
        b.timestamp_unix - a.timestamp_unix || b.sequence - a.sequence
    )
    .map((e) => ({
      seq: e.sequence,
      tsEpochSecs: e.timestamp_unix,
      eventType: e.event_type ?? "",
      context: e.context ?? "",
    }));

  // Build systemInfo from diagnostics if available
  const sysInfoRaw =
    (detail.diagnostics?.system_info as Record<string, unknown>) ?? {};
  const systemInfo: ServerDetailPageProps["server"]["systemInfo"] = {
    version: String(sysInfoRaw.version ?? agent?.version ?? ""),
    targetArch: String(sysInfoRaw.target_arch ?? ""),
    targetOs: String(sysInfoRaw.target_os ?? ""),
    buildProfile: String(sysInfoRaw.build_profile ?? ""),
    gitCommit: sysInfoRaw.git_commit
      ? String(sysInfoRaw.git_commit)
      : undefined,
    buildTimeUtc: sysInfoRaw.build_time_utc
      ? String(sysInfoRaw.build_time_utc)
      : undefined,
    uptimeSeconds: runtime?.uptime_seconds ?? 0,
    configHash: String(sysInfoRaw.config_hash ?? ""),
    configPath: String(sysInfoRaw.config_path ?? ""),
    configReloadCount:
      typeof sysInfoRaw.config_reload_count === "number"
        ? sysInfoRaw.config_reload_count
        : 0,
  };

  return {
    id: agent?.id ?? "",
    name: agent?.node_name ?? "",
    status: resolveAgentSeverity(agent ?? ({} as Agent)),
    systemInfo,
    gates,
    dcs,
    connections,
    summary,
    upstreams,
    events,
    eventsDroppedTotal: 0,
  };
}
