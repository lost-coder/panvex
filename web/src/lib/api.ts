import { resolveAPIBasePath, resolveConfiguredRootPath } from "./runtime-path";

export type MeResponse = {
  id: string;
  username: string;
  role: string;
  totp_enabled: boolean;
};

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
  dcs: Array<{
    dc: number;
    available_endpoints: number;
    available_pct: number;
    required_writers: number;
    alive_writers: number;
    coverage_pct: number;
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
  }>;
  recent_events: RuntimeEvent[];
  system_load?: {
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
  };
};

export type AgentCertificateRecovery = {
  status: "allowed" | "expired" | "used" | "revoked";
  issued_at_unix: number;
  expires_at_unix: number;
  used_at_unix?: number;
  revoked_at_unix?: number;
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
  severity: "good" | "warn" | "bad";
  reason: string;
  runtime_freshness: TelemetryFreshness;
  detail_boost: TelemetryDetailBoost;
};

export type TelemetryAttentionItem = {
  agent_id: string;
  node_name: string;
  fleet_group_id: string;
  severity: "good" | "warn" | "bad";
  reason: string;
  presence_state: string;
  runtime: AgentRuntime;
  runtime_freshness: TelemetryFreshness;
  detail_boost: TelemetryDetailBoost;
};

export type TelemetryDashboardResponse = {
  fleet: FleetResponse;
  attention: TelemetryAttentionItem[];
  server_cards: TelemetryServerSummary[];
  runtime_distribution: Record<string, number>;
  recent_runtime_events: RuntimeEvent[];
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

export type Agent = {
  id: string;
  node_name: string;
  fleet_group_id: string;
  version: string;
  read_only: boolean;
  presence_state: string;
  certificate_recovery?: AgentCertificateRecovery;
  runtime: AgentRuntime;
  last_seen_at: string;
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

export type ClientIPEntry = {
  AgentID: string;
  ClientID: string;
  IPAddress: string;
  FirstSeen: string;
  LastSeen: string;
};

export type ClientIPHistoryResponse = {
  ips: ClientIPEntry[];
  total_unique: number;
};

export type FleetGroupEntry = {
  id: string;
  agent_count: number;
};

export type RetentionSettings = {
  ts_raw_seconds: number;
  ts_hourly_seconds: number;
  ts_dc_seconds: number;
  ip_history_seconds: number;
  event_history_seconds: number;
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

export type Job = {
  id: string;
  action: string;
  target_agent_ids: string[];
  ttl: number;
  idempotency_key: string;
  actor_id: string;
  status: string;
  created_at: string;
};

export type AuditEvent = {
  id: string;
  actor_id: string;
  action: string;
  target_id: string;
  created_at: string;
  details: Record<string, unknown>;
};

export type MetricSnapshot = {
  id: string;
  agent_id: string;
  instance_id: string;
  captured_at: string;
  values: Record<string, number>;
};

export type EnrollmentTokenResponse = {
  value: string;
  panel_url: string;
  fleet_group_id: string;
  issued_at_unix: number;
  expires_at_unix: number;
  ca_pem: string;
};

export type EnrollmentTokenListItem = {
  value: string;
  panel_url: string;
  fleet_group_id: string;
  status: "active" | "expired" | "consumed" | "revoked";
  issued_at_unix: number;
  expires_at_unix: number;
  consumed_at_unix?: number;
  revoked_at_unix?: number;
};

export type TotpSetupResponse = {
  secret: string;
  otpauth_url: string;
};

export type TotpStatusResponse = {
  totp_enabled: boolean;
};

export type LocalUser = {
  id: string;
  username: string;
  role: string;
  totp_enabled: boolean;
};

export type ClientListItem = {
  id: string;
  name: string;
  enabled: boolean;
  assigned_nodes_count: number;
  expiration_rfc3339: string;
  traffic_used_bytes: number;
  unique_ips_used: number;
  active_tcp_conns: number;
  data_quota_bytes: number;
  last_deploy_status: string;
};

export type ClientDeployment = {
  agent_id: string;
  desired_operation: string;
  status: string;
  last_error: string;
  connection_link: string;
  last_applied_at_unix: number;
  updated_at_unix: number;
};

export type Client = {
  id: string;
  name: string;
  secret: string;
  user_ad_tag: string;
  enabled: boolean;
  traffic_used_bytes: number;
  unique_ips_used: number;
  active_tcp_conns: number;
  max_tcp_conns: number;
  max_unique_ips: number;
  data_quota_bytes: number;
  expiration_rfc3339: string;
  fleet_group_ids: string[];
  agent_ids: string[];
  deployments: ClientDeployment[];
  created_at_unix: number;
  updated_at_unix: number;
  deleted_at_unix: number;
};

export type ClientInput = {
  name: string;
  enabled?: boolean;
  user_ad_tag?: string;
  max_tcp_conns: number;
  max_unique_ips: number;
  data_quota_bytes: number;
  expiration_rfc3339: string;
  fleet_group_ids: string[];
  agent_ids: string[];
};

export type DiscoveredClientConflict = {
  type: "same_secret_different_names" | "same_name_different_secrets";
  related_ids: string[];
};

export type DiscoveredClient = {
  id: string;
  agent_id: string;
  node_name: string;
  client_name: string;
  status: "pending_review" | "adopted" | "ignored";
  total_octets: number;
  current_connections: number;
  active_unique_ips: number;
  connection_link: string;
  max_tcp_conns: number;
  max_unique_ips: number;
  data_quota_bytes: number;
  expiration: string;
  discovered_at_unix: number;
  updated_at_unix: number;
  conflicts?: DiscoveredClientConflict[];
};

export type AdoptDiscoveredClientResponse = {
  client_id: string;
  name: string;
};

export type PanelSettingsResponse = {
  http_public_url: string;
  http_root_path: string;
  grpc_public_endpoint: string;
  http_listen_address: string;
  grpc_listen_address: string;
  tls_mode: "proxy" | "direct";
  tls_cert_file: string;
  tls_key_file: string;
  runtime_source: "legacy" | "config_file";
  runtime_config_path: string;
  updated_at_unix: number;
  restart: {
    supported: boolean;
    pending: boolean;
    state: "ready" | "pending" | "unavailable";
  };
};

export type AppearanceSettingsResponse = {
  theme: "system" | "light" | "dark";
  density: "comfortable" | "compact";
  help_mode: "off" | "basic" | "full";
  updated_at_unix: number;
};

export type JobCreateInput = {
  action: string;
  target_agent_ids: string[];
  idempotency_key: string;
  ttl_seconds: number;
};

export type CreateUserInput = {
  username: string;
  role: string;
  password: string;
};

export type UpdateUserInput = {
  username: string;
  role: string;
  new_password?: string;
};

export const configuredRootPath = resolveConfiguredRootPath();
export const apiBasePath = resolveAPIBasePath(configuredRootPath);

export async function api<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(path, {
    credentials: "include",
    headers: {
      "Content-Type": "application/json",
      ...(init?.headers ?? {})
    },
    ...init
  });

  if (response.status === 204) {
    return undefined as T;
  }

  if (!response.ok) {
    let message = `Request failed with status ${response.status}`;
    try {
      const payload = (await response.json()) as { error?: string };
      if (payload.error) {
        message = payload.error;
      }
    } catch {
      // Ignore JSON parsing failures for error responses.
    }
    throw new Error(message);
  }

  return (await response.json()) as T;
}

export const apiClient = {
  login: (payload: { username: string; password: string; totp_code?: string }) =>
    api<{ status: string }>(`${apiBasePath}/auth/login`, {
      method: "POST",
      body: JSON.stringify(payload)
    }),
  logout: () =>
    api<void>(`${apiBasePath}/auth/logout`, {
      method: "POST"
    }),
  me: () => api<MeResponse>(`${apiBasePath}/auth/me`),
  startTotpSetup: () =>
    api<TotpSetupResponse>(`${apiBasePath}/auth/totp/setup`, {
      method: "POST"
    }),
  enableTotp: (payload: { password: string; totp_code: string }) =>
    api<TotpStatusResponse>(`${apiBasePath}/auth/totp/enable`, {
      method: "POST",
      body: JSON.stringify(payload)
    }),
  disableTotp: (payload: { password: string; totp_code: string }) =>
    api<TotpStatusResponse>(`${apiBasePath}/auth/totp/disable`, {
      method: "POST",
      body: JSON.stringify(payload)
    }),
  controlRoom: () => api<ControlRoomResponse>(`${apiBasePath}/control-room`),
  telemetryDashboard: () => api<TelemetryDashboardResponse>(`${apiBasePath}/telemetry/dashboard`),
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
  agents: () => api<Agent[]>(`${apiBasePath}/agents`),
  instances: () => api<Instance[]>(`${apiBasePath}/instances`),
  users: () => api<LocalUser[]>(`${apiBasePath}/users`),
  clients: () => api<ClientListItem[]>(`${apiBasePath}/clients`),
  client: (clientID: string) => api<Client>(`${apiBasePath}/clients/${clientID}`),
  createClient: (payload: ClientInput) =>
    api<Client>(`${apiBasePath}/clients`, {
      method: "POST",
      body: JSON.stringify(payload)
    }),
  updateClient: (clientID: string, payload: ClientInput) =>
    api<Client>(`${apiBasePath}/clients/${clientID}`, {
      method: "PUT",
      body: JSON.stringify(payload)
    }),
  rotateClientSecret: (clientID: string) =>
    api<Client>(`${apiBasePath}/clients/${clientID}/rotate-secret`, {
      method: "POST"
    }),
  deleteClient: (clientID: string) =>
    api<void>(`${apiBasePath}/clients/${clientID}`, {
      method: "DELETE"
    }),
  panelSettings: () => api<PanelSettingsResponse>(`${apiBasePath}/settings/panel`),
  appearanceSettings: () => api<AppearanceSettingsResponse>(`${apiBasePath}/settings/appearance`),
  updateAppearanceSettings: (payload: {
    theme: "system" | "light" | "dark";
    density: "comfortable" | "compact";
    help_mode: "off" | "basic" | "full";
  }) =>
    api<AppearanceSettingsResponse>(`${apiBasePath}/settings/appearance`, {
      method: "PUT",
      body: JSON.stringify(payload)
    }),
  updatePanelSettings: (payload: {
    http_public_url: string;
    grpc_public_endpoint: string;
  }) =>
    api<PanelSettingsResponse>(`${apiBasePath}/settings/panel`, {
      method: "PUT",
      body: JSON.stringify(payload)
    }),
  restartPanel: () =>
    api<PanelSettingsResponse>(`${apiBasePath}/settings/panel/restart`, {
      method: "POST"
    }),
  createUser: (payload: CreateUserInput) =>
    api<LocalUser>(`${apiBasePath}/users`, {
      method: "POST",
      body: JSON.stringify(payload)
    }),
  updateUser: (userID: string, payload: UpdateUserInput) =>
    api<LocalUser>(`${apiBasePath}/users/${userID}`, {
      method: "PUT",
      body: JSON.stringify(payload)
    }),
  deleteUser: (userID: string) =>
    api<void>(`${apiBasePath}/users/${userID}`, {
      method: "DELETE"
    }),
  resetUserTotp: (userID: string) =>
    api<void>(`${apiBasePath}/users/${userID}/totp/reset`, {
      method: "POST"
    }),
  jobs: () => api<Job[]>(`${apiBasePath}/jobs`),
  audit: () => api<AuditEvent[]>(`${apiBasePath}/audit`),
  metrics: () => api<MetricSnapshot[]>(`${apiBasePath}/metrics`),
  createJob: (payload: JobCreateInput) =>
    api<Job>(`${apiBasePath}/jobs`, {
      method: "POST",
      body: JSON.stringify(payload)
    }),
  createEnrollmentToken: (payload: {
    fleet_group_id: string;
    ttl_seconds: number;
  }) =>
    api<EnrollmentTokenResponse>(`${apiBasePath}/agents/enrollment-tokens`, {
      method: "POST",
      body: JSON.stringify(payload)
    }),
  allowAgentCertificateRecovery: (agentID: string, payload?: { ttl_seconds?: number }) =>
    api<AgentCertificateRecovery>(`${apiBasePath}/agents/${agentID}/certificate-recovery-grants`, {
      method: "POST",
      body: JSON.stringify(payload ?? {})
    }),
  revokeAgentCertificateRecovery: (agentID: string) =>
    api<AgentCertificateRecovery>(`${apiBasePath}/agents/${agentID}/certificate-recovery-grants/revoke`, {
      method: "POST"
    }),
  listEnrollmentTokens: () => api<EnrollmentTokenListItem[]>(`${apiBasePath}/agents/enrollment-tokens`),
  revokeEnrollmentToken: (value: string) =>
    api<void>(`${apiBasePath}/agents/enrollment-tokens/${value}/revoke`, {
      method: "POST"
    }),
  discoveredClients: () => api<DiscoveredClient[]>(`${apiBasePath}/discovered-clients`),
  adoptDiscoveredClient: (id: string) =>
    api<AdoptDiscoveredClientResponse>(`${apiBasePath}/discovered-clients/${id}/adopt`, {
      method: "POST"
    }),
  ignoreDiscoveredClient: (id: string) =>
    api<void>(`${apiBasePath}/discovered-clients/${id}/ignore`, {
      method: "POST"
    }),
  renameAgent: (agentID: string, nodeName: string) =>
    api<Agent>(`${apiBasePath}/agents/${agentID}`, {
      method: "PATCH",
      body: JSON.stringify({ node_name: nodeName })
    }),
  deregisterAgent: (agentID: string) =>
    api<void>(`${apiBasePath}/agents/${agentID}`, {
      method: "DELETE"
    }),
  fleetGroups: () => api<FleetGroupEntry[]>(`${apiBasePath}/fleet-groups`),
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
  clientIPHistory: (clientID: string, from?: string, to?: string) => {
    const params = new URLSearchParams();
    if (from) params.set("from", from);
    if (to) params.set("to", to);
    const qs = params.toString();
    return api<ClientIPHistoryResponse>(`${apiBasePath}/clients/${clientID}/history/ips${qs ? "?" + qs : ""}`);
  },
  getRetentionSettings: () => api<RetentionSettings>(`${apiBasePath}/settings/retention`),
  putRetentionSettings: (settings: RetentionSettings) =>
    api<RetentionSettings>(`${apiBasePath}/settings/retention`, {
      method: "PUT",
      body: JSON.stringify(settings),
    }),
};
