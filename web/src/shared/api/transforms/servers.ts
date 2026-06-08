import { deriveNodeState } from "@/ui";

import type {
  AgentConnectionData,
  InitCardProps,
  ServerDetailPageProps,
  ServerListItem,
} from "@/shared/api/types-pages/pages";
import type {
  TelemetryServersResponse,
  TelemetryServerDetailResponse,
  TelemetryServerSummary,
  Agent,
} from "../api";

type Severity = "ok" | "warn" | "error";

function num(v: unknown): number {
  return typeof v === "number" ? v : 0;
}

// str narrows an unknown diagnostic blob field to a string. The
// previous String(v ?? "") pattern coerced objects to "[object Object]"
// (Sonar S6551); this helper returns "" for anything that is not
// already a string.
function str(v: unknown): string {
  return typeof v === "string" ? v : "";
}

function pct1(v: number | undefined | null): number {
  return v ? Math.round((v ?? 0) * 10) / 10 : 0;
}

function ms1(v: number | undefined | null): number | undefined {
  return v && v > 0 ? Math.round(v * 10) / 10 : undefined;
}

function load2(v: number | undefined | null): number {
  return v ? Math.round((v ?? 0) * 100) / 100 : 0;
}

function rec(v: unknown): Record<string, unknown> {
  return (v != null && typeof v === "object" && !Array.isArray(v))
    ? v as Record<string, unknown>
    : {};
}

function mapSeverity(
  s: "good" | "ok" | "warn" | "critical" | "bad",
): Severity {
  if (s === "good" || s === "ok") return "ok";
  if (s === "bad" || s === "critical") return "error";
  return "warn";
}

function resolveAgentSeverity(agent: Agent): Severity {
  if (agent.presence_state === "offline") return "error";
  if (agent.presence_state === "degraded" || agent.runtime?.degraded)
    return "warn";
  return "ok";
}

function initElapsedSecs(completedAtUnix: number, remainingSeconds: number): number {
  if (completedAtUnix > 0) return 0;
  if (remainingSeconds > 0) return remainingSeconds;
  return 0;
}

function presenceStateToUI(state: Agent["presence_state"]): "online" | "degraded" | "offline" {
  if (state === "online") return "online";
  if (state === "degraded") return "degraded";
  return "offline";
}

function dcCoverageStatus(coveragePct: number): Severity {
  if (coveragePct >= 99.5) return "ok";
  if (coveragePct > 0) return "warn";
  return "error";
}

function mapDcs(
  dcs: Agent["runtime"]["dcs"]
): Array<{ dc: number; status: Severity; rttMs: number | null; coveragePct?: number; load?: number }> {
  return (dcs ?? []).map((dc) => ({
    dc: dc.dc,
    status: dcCoverageStatus(dc.coverage_pct),
    rttMs: ms1(dc.rtt_ms) ?? null,
    coveragePct: pct1(dc.coverage_pct),
    load: load2(dc.load),
  }));
}

function summaryToListItem(card: TelemetryServerSummary): ServerListItem {
  const agent = card.agent;
  const runtime = agent?.runtime;
  const dcs = runtime?.dcs ?? [];
  // Healthy DC = full coverage (>= 99.5 %), matching the dcCoverageStatus
  // threshold used in the detail page. We keep totals in sync with the
  // payload Telemt actually exposes — operators in ME mode otherwise saw
  // a misleading "1/1 upstreams" instead of "N/M datacenters".
  const totalDcs = dcs.length;
  const healthyDcs = dcs.filter((dc) => dc.coverage_pct >= 99.5).length;
  return {
    id: agent?.id ?? "",
    name: agent?.node_name ?? "",
    status: mapSeverity(card.severity),
    state: deriveNodeState({
      severity: card.severity,
      presenceState: agent?.presence_state ?? "online",
      telemtUnreachable: runtime?.telemt_unreachable ?? false,
      reason: card.reason ?? "",
    }),
    reason: card.reason ?? "",
    connections: runtime?.current_connections ?? 0,
    // active_users / configured_users come straight from Telemt; the
    // previous fallback (connections × 2) was a placeholder that
    // surfaced as nonsense like "24 / 48" during normal operation.
    usersOnline: runtime?.active_users ?? 0,
    usersTotal: runtime?.configured_users ?? 0,
    // Per-agent client-traffic sum projected by the panel. See
    // telemetryServerSummary.TrafficBytes on the backend.
    trafficBytes: card.traffic_bytes ?? 0,
    cpuPct: pct1(runtime?.system_load?.cpu_usage_pct),
    memPct: pct1(runtime?.system_load?.memory_usage_pct),
    dcCoveragePct: pct1(runtime?.dc_coverage_pct),
    uptimeSeconds: runtime?.uptime_seconds ?? 0,
    fleetGroupId: agent?.fleet_group_id ?? "",
    dcs: mapDcs(runtime?.dcs ?? []),
    // Direct-mode panel signals (Phase 7). Pass through the raw severity
    // from /telemetry/servers so the Transport badge can render the full
    // ok/warn/critical/bad vocabulary, while `status` retains the legacy
    // ok/warn/error mapping the rest of the row uses.
    useMiddleProxy: runtime?.use_middle_proxy ?? false,
    meRuntimeReady: runtime?.me_runtime_ready ?? false,
    me2dcFallbackEnabled: runtime?.me2dc_fallback_enabled ?? false,
    healthyUpstreams: runtime?.healthy_upstreams ?? 0,
    totalUpstreams: runtime?.total_upstreams ?? 0,
    healthyDcs,
    totalDcs,
    severity: card.severity === "good" ? "ok" : card.severity,
    telemtUnreachable: runtime?.telemt_unreachable ?? false,
    telemtUnreachableSinceUnix: runtime?.telemt_unreachable_since_unix ?? 0,
  };
}

export function transformServerList(
  raw: TelemetryServersResponse
): ServerListItem[] {
  return (raw.servers ?? []).map(summaryToListItem);
}

/** Extract a map of agent id -> version string for update comparison. */
export function extractAgentVersions(
  raw: TelemetryServersResponse
): Record<string, string> {
  const map: Record<string, string> = {};
  for (const s of raw.servers ?? []) {
    if (s.agent?.id && s.agent.version) {
      map[s.agent.id] = s.agent.version;
    }
  }
  return map;
}

export function transformInitState(
  raw: TelemetryServerDetailResponse
): InitCardProps | undefined {
  const iw = raw.initialization_watch;
  if (!iw?.visible || iw.mode !== "active") return undefined;

  // Don't show init card if both init and startup report ready —
  // backend may still flag visible=true due to gate delays
  const initReady = !iw.initialization_status || iw.initialization_status === "ready";
  const startupReady = !iw.startup_status || iw.startup_status === "ready";
  if (initReady && startupReady) return undefined;

  const runtime = raw.server?.agent?.runtime;
  return {
    stage: iw.initialization_stage || iw.startup_stage || "initializing",
    progressPct: iw.initialization_progress_pct ?? iw.startup_progress_pct ?? 0,
    attempt: 1,
    retryLimit: 1,
    elapsedSecs: initElapsedSecs(iw.completed_at_unix, iw.remaining_seconds),
    degraded: runtime?.degraded ?? false,
  };
}

function transformMePool(
  blob: Record<string, unknown>,
  runtime: Agent["runtime"] | undefined,
): ServerDetailPageProps["server"]["mePool"] {
  if (blob?.enabled !== true) return undefined;

  const data = rec(blob.data);
  const generations = rec(data.generations);
  const hardswap = rec(data.hardswap);
  const writers = rec(data.writers);
  const contour = rec(writers.contour);
  const health = rec(writers.health);
  const refill = rec(data.refill);
  const byDcRaw = Array.isArray(refill.by_dc) ? refill.by_dc : [];

  // Prefer real ME writers summary from Telemt /v1/stats/me-writers when available.
  const mws = runtime?.me_writers_summary;
  const aliveWriters = mws?.alive_writers ?? num(writers.alive_non_draining);
  const totalDcGroups = runtime?.dcs?.length ?? 0;

  return {
    enabled: true,
    summary: {
      aliveWriters,
      availableEndpoints: mws?.available_endpoints ?? 0,
      availablePct: pct1(mws ? (mws.available_endpoints / Math.max(mws.configured_endpoints, 1)) * 100 : 0),
      configuredDcGroups: totalDcGroups,
      configuredEndpoints: mws?.configured_endpoints ?? 0,
      coveragePct: pct1(mws?.coverage_pct ?? runtime?.dc_coverage_pct),
      freshAliveWriters: mws?.fresh_alive_writers ?? aliveWriters,
      freshCoveragePct: pct1(mws?.fresh_coverage_pct ?? mws?.coverage_pct ?? runtime?.dc_coverage_pct),
      requiredWriters: mws?.required_writers ?? 0,
    },
    generations: {
      active: num(generations.active_generation),
      warm: num(generations.warm_generation),
      pendingHardswap: num(generations.pending_hardswap_generation),
      pendingHardswapAgeSecs: num(generations.pending_hardswap_age_secs) || undefined,
      drainingGenerations: Array.isArray(generations.draining_generations)
        ? generations.draining_generations.map(Number)
        : [],
    },
    hardswap: {
      enabled: hardswap.enabled === true,
      pending: hardswap.pending === true,
    },
    contour: {
      active: num(contour.active),
      warm: num(contour.warm),
      draining: num(contour.draining),
    },
    writersHealth: {
      healthy: num(health.healthy),
      degraded: num(health.degraded),
      draining: num(health.draining),
    },
    refill: {
      inflightEndpoints: num(refill.inflight_endpoints_total),
      inflightDcs: num(refill.inflight_dc_total),
      byDc: byDcRaw.map((e: Record<string, unknown>) => ({
        dc: num(e.dc),
        family: str(e.family),
        inflight: num(e.inflight),
      })),
    },
    writersList: [],
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
    rerouteActive: runtime?.reroute_active ?? false,
    startupStatus: runtime?.startup_status ?? "",
    startupProgressPct: runtime?.startup_progress_pct ?? 0,
    degraded: runtime?.degraded ?? false,
    readOnly: agent?.read_only ?? false,
  };

  // Build a lookup from dcs_detail diagnostics blob for endpoints/floor data
  const dcsDetailBlob = detail.diagnostics?.dcs_detail as Record<string, unknown> | undefined;
  const rawDcsDetail = dcsDetailBlob?.dcs;
  const dcsDetailArray: Record<string, unknown>[] = Array.isArray(rawDcsDetail) ? rawDcsDetail : [];
  const dcsDetailMap = new Map<number, Record<string, unknown>>();
  for (const d of dcsDetailArray) {
    if (typeof d.dc === "number") dcsDetailMap.set(d.dc, d);
  }

  const dcs: ServerDetailPageProps["server"]["dcs"] = (
    runtime?.dcs ?? []
  ).map((dc) => {
    const detail = dcsDetailMap.get(dc.dc);
    const rawEndpoints = detail?.endpoints;
    const endpoints: string[] = Array.isArray(rawEndpoints) ? rawEndpoints : [];
    const rawEndpointWriters = detail?.endpoint_writers;
    const endpointWriters = Array.isArray(rawEndpointWriters)
      ? (rawEndpointWriters as Array<Record<string, unknown>>).map((ew) => ({
          endpoint: str(ew.endpoint),
          activeWriters: num(ew.active_writers),
        }))
      : [];
    return {
      dc: dc.dc,
      endpoints,
      endpointWriters,
      availableEndpoints: dc.available_endpoints ?? 0,
      availablePct: pct1(dc.available_pct),
      requiredWriters: dc.required_writers ?? 0,
      aliveWriters: dc.alive_writers ?? 0,
      coveragePct: pct1(dc.coverage_pct),
      floorMin: num(detail?.floor_min),
      floorTarget: num(detail?.floor_target),
      floorMax: num(detail?.floor_max),
      floorCapped: detail?.floor_capped === true,
      rttMs: ms1(dc.rtt_ms),
      load: load2(dc.load),
    };
  });

  const connections: ServerDetailPageProps["server"]["connections"] = {
    current: runtime?.current_connections ?? 0,
    currentMe: runtime?.current_connections_me ?? 0,
    currentDirect: runtime?.current_connections_direct ?? 0,
    activeUsers: runtime?.active_users ?? 0,
    staleCacheUsed: runtime?.stale_cache_used ?? false,
    topByConnections: (runtime?.top_by_connections ?? []).map((e) => ({
      username: e.username,
      connections: e.connections,
      octets: 0,
    })),
    topByThroughput: (runtime?.top_by_throughput ?? []).map((e) => ({
      username: e.username,
      connections: 0,
      octets: e.throughput_bytes,
    })),
  };

  const summary: ServerDetailPageProps["server"]["summary"] = {
    connectionsTotal: runtime?.connections_total ?? 0,
    connectionsBadTotal: runtime?.connections_bad_total ?? 0,
    handshakeTimeoutsTotal: runtime?.handshake_timeouts_total ?? 0,
    configuredUsers: runtime?.configured_users ?? 0,
    connectionsBadByClass:
      runtime?.connections_bad_by_class?.map((row) => ({
        class: row.class,
        total: row.total,
      })) ?? [],
    handshakeFailuresByClass:
      runtime?.handshake_failures_by_class?.map((row) => ({
        class: row.class,
        total: row.total,
      })) ?? [],
  };

  const upstreams: ServerDetailPageProps["server"]["upstreams"] = (
    runtime?.upstreams ?? []
  ).map((u) => ({
    upstreamId: u.upstream_id,
    routeKind: u.route_kind ?? "direct",
    address: u.address ?? "",
    weight: u.weight ?? 1,
    healthy: u.healthy ?? false,
    fails: u.fails ?? 0,
    lastCheckAgeSecs: u.last_check_age_secs ?? 0,
    effectiveLatencyMs: ms1(u.effective_latency_ms),
    dc: [],
  }));

  // Direct-mode panel summary. Backend emits the 5m fail-rate + lifetime
  // connect counters at the runtime root (Phase 5). We project them into
  // a ServerUpstreamSummaryData so the DirectRelay layouts can render
  // without falling back to "unknown" for every value.
  // Prefer the panel-authoritative totals (sourced from Telemt 3.4.7+ via
  // the agent gateway) so the summary matches Telemt's view of the
  // configured fleet exactly. Fall back to row-counting only when the
  // agent or Telemt is too old to surface the totals.
  const healthyTotal = runtime?.healthy_upstreams ?? upstreams.filter((u) => u.healthy).length;
  const configuredTotal = runtime?.total_upstreams ?? upstreams.length;
  const directTotal =
    runtime?.direct_upstreams ?? upstreams.filter((u) => u.routeKind === "direct").length;
  const socks4Total =
    runtime?.socks4_upstreams ?? upstreams.filter((u) => u.routeKind === "socks4").length;
  const socks5Total =
    runtime?.socks5_upstreams ?? upstreams.filter((u) => u.routeKind === "socks5").length;
  const shadowsocksTotal =
    runtime?.shadowsocks_upstreams ??
    upstreams.filter((u) => u.routeKind === "shadowsocks").length;
  const upstreamSummary: ServerDetailPageProps["server"]["upstreamSummary"] = {
    configuredTotal,
    healthyTotal,
    unhealthyTotal: Math.max(0, configuredTotal - healthyTotal),
    directTotal,
    socks4Total,
    socks5Total,
    shadowsocksTotal,
    failRatePct5m: runtime?.fail_rate_pct_5m ?? 0,
    failRateKnown: runtime?.fail_rate_known ?? false,
    connectAttemptTotal: runtime?.connect_attempt_total ?? 0,
    connectSuccessTotal: runtime?.connect_success_total ?? 0,
    connectFailTotal: runtime?.connect_fail_total ?? 0,
    connectFailfastTotal: runtime?.connect_failfast_total ?? 0,
  };

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
  const sysInfoRaw = detail.diagnostics?.system_info ?? {};
  const systemInfo: ServerDetailPageProps["server"]["systemInfo"] = {
    version: str(sysInfoRaw.version) || str(agent?.version),
    targetArch: str(sysInfoRaw.target_arch),
    targetOs: str(sysInfoRaw.target_os),
    buildProfile: str(sysInfoRaw.build_profile),
    gitCommit: str(sysInfoRaw.git_commit) || undefined,
    buildTimeUtc: str(sysInfoRaw.build_time_utc) || undefined,
    uptimeSeconds: runtime?.uptime_seconds ?? 0,
    configHash: str(sysInfoRaw.config_hash),
    configPath: str(sysInfoRaw.config_path),
    configReloadCount:
      typeof sysInfoRaw.config_reload_count === "number"
        ? sysInfoRaw.config_reload_count
        : 0,
  };

  const mePool = transformMePool(detail.diagnostics?.me_pool, runtime);

  const useMiddleProxy = runtime?.use_middle_proxy ?? false;
  const meRuntimeReady = runtime?.me_runtime_ready ?? false;
  const me2dcFallbackEnabled = runtime?.me2dc_fallback_enabled ?? false;
  // Phase 5: API surfaces fallback_entered_at_unix when the panel sees
  // an agent in ME->DC fallback. transportMode stays derived from
  // use_middle_proxy until the persisted agent.transport_mode field is
  // wired into the AgentRuntime payload (cleanup follow-up).
  const transportMode: ServerDetailPageProps["server"]["transportMode"] =
    useMiddleProxy ? "middle_proxy" : "direct";
  const fallbackEnteredAtUnix = runtime?.fallback_entered_at_unix ?? null;

  return {
    id: agent?.id ?? "",
    name: agent?.node_name ?? "",
    status: resolveAgentSeverity(agent ?? ({} as Agent)),
    systemInfo,
    gates,
    dcs,
    connections,
    summary,
    mePool,
    upstreams,
    upstreamSummary,
    events,
    eventsDroppedTotal: 0,
    useMiddleProxy,
    meRuntimeReady,
    me2dcFallbackEnabled,
    transportMode,
    fallbackEnteredAtUnix,
    telemtUnreachable: runtime?.telemt_unreachable ?? false,
    telemtUnreachableSinceUnix: runtime?.telemt_unreachable_since_unix ?? 0,
  };
}

// Agent mTLS certificates have a 30-day validity period.
const CERT_LIFETIME_DAYS = 30;

export function transformAgentConnection(
  agent: Agent | undefined,
): AgentConnectionData | undefined {
  if (!agent) return undefined;

  const lastSeen = agent.last_seen_at
    ? new Date(agent.last_seen_at)
    : new Date();

  // Use real certificate dates from the API when available, falling back to
  // an approximation for agents enrolled before the cert_dates migration.
  const now = new Date();
  const certIssuedAt = agent.cert_issued_at ? new Date(agent.cert_issued_at) : lastSeen;
  const certExpiresAt = agent.cert_expires_at
    ? new Date(agent.cert_expires_at)
    : new Date(certIssuedAt.getTime() + CERT_LIFETIME_DAYS * 24 * 60 * 60 * 1000);
  const remainingMs = certExpiresAt.getTime() - now.getTime();
  const remainingDays = Math.max(0, Math.ceil(remainingMs / (24 * 60 * 60 * 1000)));

  const recovery = agent.certificate_recovery;

  return {
    presenceState: presenceStateToUI(agent.presence_state),
    lastSeenAt: formatLastSeen(lastSeen),
    agentId: agent.id,
    version: agent.version || "unknown",
    fleetGroup: agent.fleet_group_id || "default",
    certificate: {
      issuedAt: certIssuedAt.toLocaleDateString("en-US", { month: "short", day: "numeric", year: "numeric" }),
      expiresAt: certExpiresAt.toLocaleDateString("en-US", { month: "short", day: "numeric", year: "numeric" }),
      remainingDays,
    },
    recoveryGrant: recovery && recovery.status !== "expired"
      ? {
          status: recovery.status as "allowed" | "used" | "revoked",
          expiresAtUnix: recovery.expires_at_unix,
        }
      : undefined,
  };
}

function formatLastSeen(date: Date): string {
  const now = Date.now();
  const diffSecs = Math.floor((now - date.getTime()) / 1000);
  if (diffSecs < 10) return "just now";
  if (diffSecs < 60) return `${diffSecs}s ago`;
  if (diffSecs < 3600) return `${Math.floor(diffSecs / 60)}m ago`;
  if (diffSecs < 86400) return `${Math.floor(diffSecs / 3600)}h ago`;
  return `${Math.floor(diffSecs / 86400)}d ago`;
}
