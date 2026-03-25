import type {
  Agent,
  ClientListItem,
  ControlRoomResponse,
  RuntimeEvent,
} from "../../lib/api";

export type FleetKpiSummary = {
  totalServers: number;
  onlineServers: number;
  degradedServers: number;
  offlineServers: number;
  totalClients: number;
  activeConnections: number;
  totalTrafficBytes: number;
  dcCoveragePct: number;
};

export type FleetDcCoverageState = "ok" | "partial" | "down";

export type FleetDcCoverageRow = {
  dc: number;
  serverCount: number;
  averageCoveragePct: number;
  averageRttMs: number;
  health: FleetDcCoverageState;
};

export type FleetDcCoverageSummary = {
  totalDcCount: number;
  okCount: number;
  partialCount: number;
  downCount: number;
  rows: FleetDcCoverageRow[];
};

export type DashboardRuntimeEvent = {
  id: string;
  agentId: string;
  agentName: string;
  timestampUnix: number;
  eventType: string;
  context: string;
  summaryText: string;
  status: "good" | "warn" | "bad" | "accent";
};

export type ServerCardSummaryMetric = {
  label: string;
  value: string;
};

export type ServerCardSummary = {
  id: string;
  nameText: string;
  locationText: string;
  statusText: string;
  statusTone: "good" | "warn" | "bad";
  metrics: ServerCardSummaryMetric[];
  dcCounts: {
    ok: number;
    partial: number;
    down: number;
  };
  dcTags: FleetDcCoverageState[];
};

export type ServerCardDcRow = {
  dc: number;
  dcText: string;
  rttText: string;
  writersText: string;
  coverageText: string;
  loadText: string;
  health: FleetDcCoverageState;
};

export type ServerCardDetails = {
  isOffline: boolean;
  rows: ServerCardDcRow[];
};

export function sumFleetTraffic(clients: ClientListItem[]): number {
  return clients.reduce((total, client) => total + client.traffic_used_bytes, 0);
}

export function buildFleetKpiSummary(
  controlRoom: ControlRoomResponse | undefined,
  agents: Agent[],
  clients: ClientListItem[]
): FleetKpiSummary {
  const fleet = controlRoom?.fleet;
  const severityCounts = countAgentSeverities(agents);
  const dcCoverageValues = agents
    .map((agent) => agent.runtime?.dc_coverage_pct ?? 0)
    .filter((coverage) => Number.isFinite(coverage));

  return {
    totalServers: fleet?.total_agents ?? agents.length,
    onlineServers: fleet?.online_agents ?? severityCounts.online,
    degradedServers: fleet?.degraded_agents ?? severityCounts.degraded,
    offlineServers: fleet?.offline_agents ?? severityCounts.offline,
    totalClients: clients.length,
    activeConnections: fleet?.live_connections ?? agents.reduce((total, agent) => total + (agent.runtime?.current_connections ?? 0), 0),
    totalTrafficBytes: sumFleetTraffic(clients),
    dcCoveragePct: dcCoverageValues.length === 0 ? 0 : Math.round(dcCoverageValues.reduce((total, coverage) => total + coverage, 0) / dcCoverageValues.length),
  };
}

export function buildFleetDcCoverageSummary(agents: Agent[]): FleetDcCoverageSummary {
  const dcMap = new Map<number, { coverage: number[]; rtts: number[]; serverCount: number; healthStates: FleetDcCoverageState[] }>();

  for (const agent of agents) {
    for (const dc of agent.runtime?.dcs ?? []) {
      const entry = dcMap.get(dc.dc) ?? { coverage: [], rtts: [], serverCount: 0, healthStates: [] };
      entry.coverage.push(dc.coverage_pct ?? 0);
      entry.rtts.push(dc.rtt_ms ?? 0);
      entry.serverCount += 1;
      entry.healthStates.push(coverageToHealth(dc.coverage_pct ?? 0));
      dcMap.set(dc.dc, entry);
    }
  }

  const rows = Array.from(dcMap.entries())
    .sort(([leftDc], [rightDc]) => leftDc - rightDc)
    .map(([dc, entry]) => {
      const averageCoveragePct = Math.round(entry.coverage.reduce((total, coverage) => total + coverage, 0) / entry.coverage.length);
      const averageRttMs = Math.round(entry.rtts.reduce((total, rtt) => total + rtt, 0) / entry.rtts.length);
      const health = resolveFleetDcHealth(entry.healthStates);

      return {
        dc,
        serverCount: entry.serverCount,
        averageCoveragePct,
        averageRttMs,
        health,
      } satisfies FleetDcCoverageRow;
    });

  const okCount = rows.filter((row) => row.health === "ok").length;
  const partialCount = rows.filter((row) => row.health === "partial").length;
  const downCount = rows.filter((row) => row.health === "down").length;

  return {
    totalDcCount: rows.length,
    okCount,
    partialCount,
    downCount,
    rows,
  };
}

export function extractRecentRuntimeEvents(
  agents: Agent[],
  limit = 20
): DashboardRuntimeEvent[] {
  const events = agents.flatMap((agent) =>
    (agent.runtime?.recent_events ?? []).map((event) => mapRuntimeEvent(agent, event))
  );

  return events
    .sort((left, right) => {
      if (right.timestampUnix !== left.timestampUnix) {
        return right.timestampUnix - left.timestampUnix;
      }

      return right.id.localeCompare(left.id);
    })
    .slice(0, limit);
}

export function sortAgentsBySeverity(agents: Agent[]): Agent[] {
  return [...agents].sort((left, right) => {
    const severityDelta = getAgentSeverityScore(right) - getAgentSeverityScore(left);
    if (severityDelta !== 0) {
      return severityDelta;
    }

    return left.node_name.localeCompare(right.node_name);
  });
}

export function buildServerCardSummary(agent: Agent): ServerCardSummary {
  const dcSummary = buildServerCardDcCounts(agent);
  const status = mapAgentStatus(agent);
  const isOffline = status.label === "Offline";

  return {
    id: agent.id,
    nameText: agent.node_name,
    locationText: agent.fleet_group_id || "Ungrouped",
    statusText: status.label,
    statusTone: status.tone,
    metrics: [
      { label: "Clients", value: isOffline ? "—" : String(agent.runtime?.active_users ?? 0) },
      { label: "CPU", value: "—" },
      { label: "Traffic", value: "—" },
    ],
    dcCounts: dcSummary.counts,
    dcTags: dcSummary.tags,
  };
}

export function buildServerCardDetails(agent: Agent): ServerCardDetails {
  const isOffline = agent.presence_state === "offline";

  if (isOffline) {
    return {
      isOffline: true,
      rows: [],
    };
  }

  const rows = [...(agent.runtime?.dcs ?? [])]
    .sort((left, right) => left.dc - right.dc)
    .map((dc) => {
      const health = coverageToHealth(dc.coverage_pct ?? 0);

      return {
        dc: dc.dc,
        dcText: String(dc.dc),
        rttText: dc.rtt_ms > 0 ? `${Math.round(dc.rtt_ms)}ms` : "—",
        writersText: `${dc.alive_writers}/${dc.required_writers}`,
        coverageText: `${Math.round(dc.coverage_pct ?? 0)}%`,
        loadText: String(dc.load),
        health,
      } satisfies ServerCardDcRow;
    });

  return {
    isOffline: false,
    rows,
  };
}

function mapRuntimeEvent(agent: Agent, event: RuntimeEvent): DashboardRuntimeEvent {
  return {
    id: `${agent.id}-${event.sequence}-${event.timestamp_unix}`,
    agentId: agent.id,
    agentName: agent.node_name,
    timestampUnix: event.timestamp_unix,
    eventType: event.event_type,
    context: event.context,
    summaryText: event.context || event.event_type.replaceAll("_", " "),
    status: mapEventStatus(event.event_type),
  };
}

function mapEventStatus(eventType: string): "good" | "warn" | "bad" | "accent" {
  const normalized = eventType.toLowerCase();

  if (
    normalized.includes("connect") ||
    normalized.includes("online") ||
    normalized.includes("join") ||
    normalized.includes("register")
  ) {
    return "good";
  }

  if (
    normalized.includes("error") ||
    normalized.includes("fail") ||
    normalized.includes("disconnect") ||
    normalized.includes("offline") ||
    normalized.includes("crash")
  ) {
    return "bad";
  }

  if (
    normalized.includes("warn") ||
    normalized.includes("timeout") ||
    normalized.includes("retry") ||
    normalized.includes("slow")
  ) {
    return "warn";
  }

  return "accent";
}

function getAgentSeverityScore(agent: Agent): number {
  if (agent.presence_state === "offline") {
    return 3;
  }

  if (agent.presence_state === "degraded") {
    return 2;
  }

  if (agent.runtime?.degraded) {
    return 2;
  }

  if (!agent.runtime?.accepting_new_connections) {
    return 2;
  }

  const coverage = agent.runtime?.dc_coverage_pct ?? 0;
  if (coverage > 0 && coverage < 100) {
    return 2;
  }

  if (
    (agent.runtime?.total_upstreams ?? 0) > 0 &&
    (agent.runtime?.healthy_upstreams ?? 0) < (agent.runtime?.total_upstreams ?? 0)
  ) {
    return 2;
  }

  return 1;
}

function mapAgentStatus(agent: Agent): { label: string; tone: "good" | "warn" | "bad" } {
  if (agent.presence_state === "offline") {
    return { label: "Offline", tone: "bad" };
  }

  if (getAgentSeverityScore(agent) > 1) {
    return { label: "Degraded", tone: "warn" };
  }

  return { label: "Online", tone: "good" };
}

function coverageToHealth(coveragePct: number): FleetDcCoverageState {
  if (coveragePct >= 99.5) {
    return "ok";
  }

  if (coveragePct > 0) {
    return "partial";
  }

  return "down";
}

function resolveFleetDcHealth(healthStates: FleetDcCoverageState[]): FleetDcCoverageState {
  if (healthStates.includes("down")) {
    return "down";
  }

  if (healthStates.includes("partial")) {
    return "partial";
  }

  return "ok";
}

function buildServerCardDcCounts(agent: Agent): {
  counts: {
    ok: number;
    partial: number;
    down: number;
  };
  tags: FleetDcCoverageState[];
} {
  const tags = [...(agent.runtime?.dcs ?? [])]
    .sort((left, right) => left.dc - right.dc)
    .map((dc) => coverageToHealth(dc.coverage_pct ?? 0));

  const counts = tags.reduce(
    (accumulator, tag) => {
      accumulator[tag] += 1;
      return accumulator;
    },
    { ok: 0, partial: 0, down: 0 }
  );

  return { counts, tags };
}

function countAgentSeverities(agents: Agent[]): {
  online: number;
  degraded: number;
  offline: number;
} {
  return agents.reduce(
    (accumulator, agent) => {
      const score = getAgentSeverityScore(agent);
      if (score >= 3) {
        accumulator.offline += 1;
      } else if (score >= 2) {
        accumulator.degraded += 1;
      } else {
        accumulator.online += 1;
      }

      return accumulator;
    },
    { online: 0, degraded: 0, offline: 0 }
  );
}
