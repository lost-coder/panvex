import { api, apiBasePath, encodeRequest } from "./http";
import {
  agentCertificateRecoveryGrantRequestSchema,
  agentListSchema,
  renameAgentRequestSchema,
} from "./schemas";

export type RuntimeEvent = {
  sequence: number;
  timestamp_unix: number;
  event_type: string;
  context: string;
};

export type AgentRuntime = {
  accepting_new_connections: boolean;
  me_runtime_ready: boolean;
  me2dc_fallback_enabled: boolean;
  use_middle_proxy: boolean;
  startup_status: string;
  startup_stage: string;
  startup_progress_pct: number;
  initialization_status: string;
  degraded: boolean;
  initialization_stage: string;
  initialization_progress_pct: number;
  transport_mode: string;
  current_connections: number;
  current_connections_me: number;
  current_connections_direct: number;
  active_users: number;
  uptime_seconds: number;
  connections_total: number;
  connections_bad_total: number;
  handshake_timeouts_total: number;
  configured_users: number;
  dc_coverage_pct: number;
  healthy_upstreams: number;
  total_upstreams: number;
  reroute_active?: boolean | undefined;
  route_mode?: string | undefined;
  me2dc_fast_enabled?: boolean | undefined;
  stale_cache_used?: boolean | undefined;
  top_by_connections?: Array<{ username: string; connections: number }> | undefined;
  top_by_throughput?: Array<{ username: string; throughput_bytes: number }> | undefined;
  dcs: Array<{
    dc: number;
    available_endpoints: number;
    available_pct: number;
    required_writers: number;
    alive_writers: number;
    coverage_pct: number;
    fresh_alive_writers: number;
    fresh_coverage_pct: number;
    rtt_ms: number;
    load: number;
  }>;
  upstreams: Array<{
    upstream_id: number;
    route_kind: string;
    address: string;
    healthy: boolean;
    fails: number;
    effective_latency_ms: number;
    weight: number;
    last_check_age_secs: number;
    scopes?: string[] | undefined;
  }>;
  lifecycle_state?: string | undefined;
  updated_at?: string | undefined;
  recent_events: RuntimeEvent[];
  system_load: {
    cpu_usage_pct: number;
    memory_used_bytes: number;
    memory_total_bytes: number;
    memory_usage_pct: number;
    disk_used_bytes: number;
    disk_total_bytes: number;
    disk_usage_pct: number;
    load_1m: number;
    load_5m: number;
    load_15m: number;
    net_bytes_sent: number;
    net_bytes_recv: number;
  };
  me_writers_summary?: {
    configured_endpoints: number;
    available_endpoints: number;
    coverage_pct: number;
    fresh_alive_writers: number;
    fresh_coverage_pct: number;
    required_writers: number;
    alive_writers: number;
  } | undefined;
};

export type AgentCertificateRecovery = {
  status: "allowed" | "expired" | "used" | "revoked";
  issued_at_unix: number;
  expires_at_unix: number;
  used_at_unix?: number | undefined;
  revoked_at_unix?: number | undefined;
};

export type Agent = {
  id: string;
  node_name: string;
  fleet_group_id: string;
  version: string;
  read_only: boolean;
  presence_state: string;
  certificate_recovery?: AgentCertificateRecovery | undefined;
  cert_issued_at?: string | undefined;
  cert_expires_at?: string | undefined;
  runtime: AgentRuntime;
  last_seen_at: string;
};

export type Instance = {
  id: string;
  agent_id: string;
  name: string;
  version: string;
  config_fingerprint: string;
  connected_users: number;
  read_only: boolean;
  updated_at: string;
};

export const serversApi = {
  agents: () => api<Agent[]>(`${apiBasePath}/agents`, undefined, agentListSchema),
  instances: () => api<Instance[]>(`${apiBasePath}/instances`),
  renameAgent: (agentID: string, nodeName: string) =>
    api<Agent>(`${apiBasePath}/agents/${agentID}`, {
      method: "PATCH",
      body: encodeRequest(
        `${apiBasePath}/agents/${agentID}`,
        renameAgentRequestSchema,
        { node_name: nodeName },
      )
    }),
  deregisterAgent: (agentID: string) =>
    api<void>(`${apiBasePath}/agents/${agentID}`, {
      method: "DELETE"
    }),
  allowAgentCertificateRecovery: (agentID: string, payload?: { ttl_seconds?: number }) =>
    api<AgentCertificateRecovery>(
      `${apiBasePath}/agents/${agentID}/certificate-recovery-grants`,
      {
        method: "POST",
        body: payload?.ttl_seconds
          ? encodeRequest(
              `${apiBasePath}/agents/${agentID}/certificate-recovery-grants`,
              agentCertificateRecoveryGrantRequestSchema,
              { ttl_seconds: payload.ttl_seconds },
            )
          : JSON.stringify({}),
      },
    ),
  revokeAgentCertificateRecovery: (agentID: string) =>
    api<AgentCertificateRecovery>(`${apiBasePath}/agents/${agentID}/certificate-recovery-grants/revoke`, {
      method: "POST"
    }),
};
