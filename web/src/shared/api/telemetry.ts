import type { Agent, AgentRuntime, RuntimeEvent } from "./servers";
import { api, apiBasePath } from "./http";
import type { AuditEvent } from "./jobs";
import { controlRoomSchema, dashboardSchema } from "./schemas";

export type FleetResponse = {
  total_agents: number;
  online_agents: number;
  degraded_agents: number;
  offline_agents: number;
  total_instances: number;
  metric_snapshots: number;
  live_connections: number;
  accepting_new_connections_agents: number;
  middle_proxy_agents: number;
  dc_issue_agents: number;
};

export type ControlRoomResponse = {
  onboarding: {
    needs_first_server: boolean;
    setup_complete: boolean;
    suggested_fleet_group_id: string;
  };
  fleet: FleetResponse;
  jobs: {
    total: number;
    queued: number;
    running: number;
    failed: number;
  };
  recent_activity: AuditEvent[];
  recent_runtime_events: RuntimeEvent[];
};

export type TelemetryFreshness = {
  state: "fresh" | "stale" | "unavailable" | "disabled" | "never_collected";
  observed_at_unix: number;
};

export type TelemetryDetailBoost = {
  active: boolean;
  expires_at_unix: number;
  remaining_seconds: number;
};

export type TelemetryDiagnosticsRefreshResponse = {
  job_id: string;
  status: string;
};

export type TelemetryServerSummary = {
  agent: Agent;
  severity: "good" | "ok" | "warn" | "critical" | "bad";
  reason: string;
  runtime_freshness: TelemetryFreshness;
  detail_boost: TelemetryDetailBoost;
};

export type TelemetryAttentionItem = {
  agent_id: string;
  node_name: string;
  fleet_group_id: string;
  severity: "good" | "ok" | "warn" | "critical" | "bad";
  reason: string;
  presence_state: string;
  runtime: AgentRuntime;
  runtime_freshness: TelemetryFreshness;
  detail_boost: TelemetryDetailBoost;
};

export type TelemetryRecentEvent = RuntimeEvent & {
  agent_id: string;
  node_name: string;
};

export type TelemetryAgentLoadSeries = {
  agent_id: string;
  cpu_pct: number[];
  mem_pct: number[];
};

export type TelemetryDashboardResponse = {
  fleet: FleetResponse;
  attention: TelemetryAttentionItem[];
  server_cards: TelemetryServerSummary[];
  runtime_distribution: Record<string, number>;
  recent_runtime_events: RuntimeEvent[];
  /** Dashboard-specific enriched feed: events tagged with originating agent. */
  recent_events: TelemetryRecentEvent[];
  /** Per-agent CPU/MEM sparkline data (oldest-first). */
  agent_load_series: TelemetryAgentLoadSeries[];
};

export type TelemetryServersResponse = {
  servers: TelemetryServerSummary[];
};

export type TelemetryServerDetailResponse = {
  server: TelemetryServerSummary;
  initialization_watch: {
    visible: boolean;
    mode: "active" | "cooldown" | "hidden";
    remaining_seconds: number;
    completed_at_unix: number;
    startup_status: string;
    startup_stage: string;
    startup_progress_pct: number;
    initialization_status: string;
    initialization_stage: string;
    initialization_progress_pct: number;
  };
  diagnostics: {
    state: string;
    state_reason: string;
    system_info: Record<string, unknown>;
    effective_limits: Record<string, unknown>;
    security_posture: Record<string, unknown>;
    minimal_all: Record<string, unknown>;
    me_pool: Record<string, unknown>;
    dcs_detail: Record<string, unknown>;
  };
  security_inventory: {
    state: string;
    state_reason: string;
    enabled: boolean;
    entries_total: number;
    entries: string[];
  };
};

export type ServerLoadPoint = {
  AgentID: string;
  CapturedAt: string;
  CPUPctAvg: number;
  CPUPctMax: number;
  MemPctAvg: number;
  MemPctMax: number;
  DiskPctAvg: number;
  DiskPctMax: number;
  Load1M: number;
  ConnectionsAvg: number;
  ConnectionsMax: number;
  ActiveUsersAvg: number;
  ActiveUsersMax: number;
  DCCoverageMinPct: number;
  HealthyUpstreams: number;
  TotalUpstreams: number;
  NetBytesSent: number;
  NetBytesRecv: number;
  SampleCount: number;
};

export type ServerLoadHistoryResponse = {
  points: ServerLoadPoint[];
  resolution: "raw" | "hourly";
};

export type DCHealthPoint = {
  AgentID: string;
  CapturedAt: string;
  DC: number;
  CoveragePctAvg: number;
  CoveragePctMin: number;
  RTTMsAvg: number;
  RTTMsMax: number;
  AliveWritersMin: number;
  RequiredWriters: number;
  SampleCount: number;
};

export type DCHealthHistoryResponse = {
  points: DCHealthPoint[];
};

export type MetricSnapshot = {
  id: string;
  agent_id: string;
  instance_id: string;
  captured_at: string;
  values: Record<string, number>;
};

export const telemetryApi = {
  controlRoom: () =>
    api<ControlRoomResponse>(`${apiBasePath}/control-room`, undefined, controlRoomSchema),
  telemetryDashboard: () =>
    api<TelemetryDashboardResponse>(`${apiBasePath}/telemetry/dashboard`, undefined, dashboardSchema),
  telemetryServers: () => api<TelemetryServersResponse>(`${apiBasePath}/telemetry/servers`),
  telemetryServer: (agentID: string) => api<TelemetryServerDetailResponse>(`${apiBasePath}/telemetry/servers/${agentID}`),
  activateTelemetryDetailBoost: (agentID: string) =>
    api<TelemetryDetailBoost>(`${apiBasePath}/telemetry/servers/${agentID}/detail-boost`, {
      method: "POST"
    }),
  refreshTelemetryDiagnostics: (agentID: string) =>
    api<TelemetryDiagnosticsRefreshResponse>(`${apiBasePath}/telemetry/servers/${agentID}/refresh-diagnostics`, {
      method: "POST"
    }),
  fleet: () => api<FleetResponse>(`${apiBasePath}/fleet`),
  metrics: () => api<MetricSnapshot[]>(`${apiBasePath}/metrics`),
  serverLoadHistory: (agentID: string, from?: string, to?: string) => {
    const params = new URLSearchParams();
    if (from) params.set("from", from);
    if (to) params.set("to", to);
    const qs = params.toString();
    return api<ServerLoadHistoryResponse>(`${apiBasePath}/telemetry/servers/${agentID}/history/load${qs ? "?" + qs : ""}`);
  },
  dcHealthHistory: (agentID: string, from?: string, to?: string) => {
    const params = new URLSearchParams();
    if (from) params.set("from", from);
    if (to) params.set("to", to);
    const qs = params.toString();
    return api<DCHealthHistoryResponse>(`${apiBasePath}/telemetry/servers/${agentID}/history/dc${qs ? "?" + qs : ""}`);
  },
};
