import type { ServerListItem } from "@panvex/ui";
import type { ServerDetailPageProps, InitCardProps, AgentConnectionData } from "@panvex/ui";
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

function rec(v: unknown): Record<string, unknown> {
  return (v != null && typeof v === "object" && !Array.isArray(v))
    ? v as Record<string, unknown>
    : {};
}

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
    rttMs: dc.rtt_ms > 0 ? Math.round(dc.rtt_ms * 10) / 10 : null,
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
    cpuPct: runtime?.system_load?.cpu_usage_pct ?? 0,
    memPct: runtime?.system_load?.memory_usage_pct ?? 0,
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
    elapsedSecs: iw.completed_at_unix > 0
      ? 0
      : iw.remaining_seconds > 0
        ? iw.remaining_seconds
        : 0,
    degraded: runtime?.degraded ?? false,
  };
}

function transformMePool(
  blob: Record<string, unknown>,
  runtime: Agent["runtime"] | undefined,
): ServerDetailPageProps["server"]["mePool"] {
  if (!blob || blob.enabled !== true) return undefined;

  const data = rec(blob.data);
  const generations = rec(data.generations);
  const hardswap = rec(data.hardswap);
  const writers = rec(data.writers);
  const contour = rec(writers.contour);
  const health = rec(writers.health);
  const refill = rec(data.refill);
  const byDcRaw = Array.isArray(refill.by_dc) ? refill.by_dc : [];

  const aliveWriters = num(writers.alive_non_draining);
  const totalDcGroups = runtime?.dcs?.length ?? 0;
  const requiredWriters = (runtime?.dcs ?? []).reduce(
    (sum, dc) => sum + (dc.required_writers ?? 0), 0,
  );

  return {
    enabled: true,
    summary: {
      aliveWriters,
      availableEndpoints: 0,
      availablePct: 0,
      configuredDcGroups: totalDcGroups,
      configuredEndpoints: 0,
      coveragePct: runtime?.dc_coverage_pct ?? 0,
      freshAliveWriters: aliveWriters,
      freshCoveragePct: runtime?.dc_coverage_pct ?? 0,
      requiredWriters,
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
        family: String(e.family ?? ""),
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
    rerouteActive: false,
    startupStatus: runtime?.startup_status ?? "",
    startupProgressPct: runtime?.startup_progress_pct ?? 0,
    degraded: runtime?.degraded ?? false,
    readOnly: agent?.read_only ?? false,
  };

  // Build a lookup from dcs_detail diagnostics blob for endpoints/floor data
  const dcsDetailBlob = detail.diagnostics?.dcs_detail as Record<string, unknown> | undefined;
  const dcsDetailArray = Array.isArray(dcsDetailBlob?.dcs) ? dcsDetailBlob!.dcs as Record<string, unknown>[] : [];
  const dcsDetailMap = new Map<number, Record<string, unknown>>();
  for (const d of dcsDetailArray) {
    if (typeof d.dc === "number") dcsDetailMap.set(d.dc, d);
  }

  const dcs: ServerDetailPageProps["server"]["dcs"] = (
    runtime?.dcs ?? []
  ).map((dc) => {
    const detail = dcsDetailMap.get(dc.dc);
    const endpoints = Array.isArray(detail?.endpoints)
      ? (detail!.endpoints as string[])
      : [];
    const endpointWriters = Array.isArray(detail?.endpoint_writers)
      ? (detail!.endpoint_writers as Array<Record<string, unknown>>).map((ew) => ({
          endpoint: String(ew.endpoint ?? ""),
          activeWriters: num(ew.active_writers),
        }))
      : [];
    return {
      dc: dc.dc,
      endpoints,
      endpointWriters,
      availableEndpoints: dc.available_endpoints ?? 0,
      availablePct: dc.available_pct ?? 0,
      requiredWriters: dc.required_writers ?? 0,
      aliveWriters: dc.alive_writers ?? 0,
      coveragePct: dc.coverage_pct ?? 0,
      floorMin: num(detail?.floor_min),
      floorTarget: num(detail?.floor_target),
      floorMax: num(detail?.floor_max),
      floorCapped: detail?.floor_capped === true,
      rttMs: dc.rtt_ms > 0 ? Math.round(dc.rtt_ms * 10) / 10 : undefined,
      load: dc.load ?? 0,
    };
  });

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

  const mePool = transformMePool(
    detail.diagnostics?.me_pool as Record<string, unknown>,
    runtime,
  );

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
    events,
    eventsDroppedTotal: 0,
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
    presenceState: (agent.presence_state === "online"
      ? "online"
      : agent.presence_state === "degraded"
        ? "degraded"
        : "offline") as AgentConnectionData["presenceState"],
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
