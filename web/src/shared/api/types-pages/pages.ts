import type { Status } from "@/ui/tokens/colors";

// Page data contract types for panvex-web integration

// --- Shared ---

/** Proxy link URLs grouped by protocol variant. */
export interface UserLinks {
  classic: string[];
  secure: string[];
  tls: string[];
}

/** @deprecated Use `Status` from `@/tokens/colors` instead */
export type Severity = Status;

export interface KpiItem {
  label: string;
  value: string;
  sub?: string | undefined;
  accent?: boolean | undefined;
  /** Sparkline data — one point per sample; omit to render without a chart. */
  series?: number[] | undefined;
  /** Human-readable delta ("+4.2% · 24h"); rendered under the value. */
  deltaLabel?: string | undefined;
  /** Controls the arrow glyph rendered next to `deltaLabel`. */
  deltaDirection?: "up" | "down" | "flat" | undefined;
  /** Coloring the value + sparkline — use for health signals, not trend. */
  tone?: "default" | "ok" | "warn" | "error" | undefined;
}

export interface TrendItem {
  label: string;
  data: number[];
  color: string;
  current: string;
}

export interface TimelineEventData {
  status: Status | "info";
  time: string;
  message: string;
  /** Originating node name — shown on a first row above the message. */
  source?: string | undefined;
}

export interface AlertData {
  severity: "warn" | "crit";
  message: string;
  source: string;
  timestamp: string;
}

// --- Dashboard ---

export interface DashboardOverviewData {
  kpis: KpiItem[];
  trends: TrendItem[];
  alerts: AlertData[];
  attentionNodes: DashboardNodeData[];
  healthyNodes: DashboardNodeData[];
}

export interface DashboardNodeData {
  id: string;
  name: string;
  status: Status;
  connections: number;
  trafficBytes: number;
  cpuPct: number;
  memPct: number;
  dcs: import("@/features/servers/ui/NodeSummaryCard").NodeDcInfo[];
  /** Recent CPU samples (oldest-first) for the dashboard sparkline. */
  cpuSeries?: number[] | undefined;
  /** Recent MEM samples (oldest-first) for the dashboard sparkline. */
  memSeries?: number[] | undefined;
}

export interface DashboardTimelineData {
  events: TimelineEventData[];
}

export interface DashboardPageProps {
  overview: DashboardOverviewData;
  timeline: DashboardTimelineData;
  onNodeClick?: ((nodeId: string) => void) | undefined;
  onNodeLinkClick?: ((nodeId: string) => void) | undefined;
  onCreate?: ((data: ClientFormData) => void | Promise<void>) | undefined;
  createLoading?: boolean | undefined;
  createError?: string | undefined;
  pendingDiscoveredCount?: number | undefined;
  onDiscoveredClick?: (() => void) | undefined;
  /** Navigates to the full Servers list — wired to the "View all →" link
   *  in the Fleet card header. Optional so unit tests can skip it. */
  onViewAllServers?: (() => void) | undefined;
}

// --- Servers ---

export interface ServerListItem {
  id: string;
  name: string;
  status: Status;
  ip?: string | undefined;
  connections: number;
  usersOnline?: number | undefined;
  usersTotal?: number | undefined;
  trafficBytes: number;
  cpuPct: number;
  memPct: number;
  dcCoveragePct: number;
  uptimeSeconds: number;
  fleetGroupId: string;
  dcs?: import("@/features/servers/ui/NodeSummaryCard").NodeDcInfo[] | undefined;
}

export type ViewMode = "cards" | "list";

/**
 * Bulk actions operators can trigger against a multi-selection of
 * servers on the Servers list. Each value maps to a backend job
 * action; the UI stays compact (reload / upgrade today, more as
 * backend support lands).
 */
export type BulkServerAction = "reload" | "selfUpdate";

export interface ServersPageProps {
  servers: ServerListItem[];
  viewMode?: ViewMode | undefined;
  autoThreshold?: number | undefined;
  fleetGroups?: FleetGroupOption[] | undefined;
  onViewModeChange?: ((mode: ViewMode) => void) | undefined;
  onServerClick?: ((serverId: string) => void) | undefined;
  onServerLinkClick?: ((serverId: string) => void) | undefined;
  onAddServer?: (() => void) | undefined;
  onManageTokens?: (() => void) | undefined;
  /**
   * Bulk-action callback — ServersContainer wires it to apiClient.createJob
   * with `target_agent_ids = selected ids`. Returning a promise lets the
   * page keep the toolbar "busy" until the backend acknowledges.
   */
  onBulkAction?: ((action: BulkServerAction, agentIds: string[]) => void | Promise<void>) | undefined;
  /** Surface backend errors from the last bulk action inside the toolbar. */
  bulkError?: string | undefined;
  /** True while apiClient.createJob is in flight. */
  bulkPending?: boolean | undefined;
}

// --- Server Detail ---

// /v1/stats/dcs → dcs[]
export interface ServerDcData {
  dc: number;
  // endpoints
  endpoints: string[];
  endpointWriters: Array<{ endpoint: string; activeWriters: number }>;
  availableEndpoints: number;
  availablePct: number;
  // writers
  requiredWriters: number;
  aliveWriters: number;
  coveragePct: number;
  freshAlivePct?: number | undefined; // fresh_coverage_pct
  // floor policy
  floorMin: number;
  floorTarget: number;
  floorMax: number;
  floorCapped: boolean;
  // perf
  rttMs?: number | undefined;
  load: number;
}

// /v1/stats/upstreams → upstreams[]
export interface ServerUpstreamData {
  upstreamId: number;
  routeKind: string; // direct | socks4 | socks5 | shadowsocks
  address: string;
  weight: number;
  healthy: boolean;
  fails: number;
  lastCheckAgeSecs: number;
  effectiveLatencyMs?: number | undefined;
  dc: Array<{ dc: number; latencyEmaMs?: number | undefined; ipPreference: string }>;
}

// /v1/runtime/connections/summary
export interface ServerConnectionsData {
  current: number;
  currentMe: number;
  currentDirect: number;
  activeUsers: number;
  staleCacheUsed: boolean;
  topByConnections: Array<{ username: string; connections: number; octets: number }>;
  topByThroughput: Array<{ username: string; connections: number; octets: number }>;
}

// /v1/stats/summary
export interface ServerSummaryData {
  connectionsTotal: number;
  connectionsBadTotal: number;
  handshakeTimeoutsTotal: number;
  configuredUsers: number;
}

// /v1/system/info
export interface ServerSystemInfoData {
  version: string;
  targetArch: string;
  targetOs: string;
  buildProfile: string;
  gitCommit?: string | undefined;
  buildTimeUtc?: string | undefined;
  uptimeSeconds: number;
  configHash: string;
  configPath: string;
  configReloadCount: number;
  lastConfigReloadEpochSecs?: number | undefined;
}

// /v1/runtime/gates + /v1/health
export interface ServerGatesData {
  acceptingNewConnections: boolean;
  meRuntimeReady: boolean;
  useMiddleProxy: boolean;
  me2dcFallbackEnabled: boolean;
  rerouteActive: boolean;
  rerouteReason?: string | undefined;
  startupStatus: string; // pending | initializing | ready | failed | skipped
  startupProgressPct: number;
  degraded: boolean; // from /v1/runtime/initialization
  readOnly: boolean; // from /v1/health
}

// /v1/stats/me-writers → writers[]
export interface ServerMeWriterData {
  writerId: number;
  dc?: number | undefined;
  endpoint: string;
  state: string; // warm | active | draining
  draining: boolean;
  degraded: boolean;
  boundClients: number;
  idleForSecs?: number | undefined;
  rttEmaMs?: number | undefined;
}

// /v1/stats/me-writers aggregate
export interface ServerMePoolData {
  enabled: boolean;
  // /v1/stats/me-writers → summary (9 fields)
  summary: {
    aliveWriters: number;
    availableEndpoints: number;
    availablePct: number;
    configuredDcGroups: number;
    configuredEndpoints: number;
    coveragePct: number;
    freshAliveWriters: number;
    freshCoveragePct: number;
    requiredWriters: number;
  };
  // /v1/runtime/me_pool_state → generations
  generations: {
    active: number;
    warm: number;
    pendingHardswap: number;
    pendingHardswapAgeSecs?: number | undefined;
    drainingGenerations: number[];
  };
  hardswap: {
    enabled: boolean;
    pending: boolean;
  };
  // /v1/runtime/me_pool_state → contour
  contour: {
    active: number;
    warm: number;
    draining: number;
  };
  // /v1/runtime/me_pool_state → writers health
  writersHealth: {
    healthy: number;
    degraded: number;
    draining: number;
  };
  refill: {
    inflightEndpoints: number;
    inflightDcs: number;
    byDc: Array<{ dc: number; family: string; inflight: number }>;
  };
  writersList: ServerMeWriterData[];
}

// /v1/runtime/me_quality
export interface ServerMeQualityData {
  enabled: boolean;
  counters: {
    idleCloseByPeerTotal: number;
    readerEofTotal: number;
    kdfDriftTotal: number;
    kdfPortOnlyDriftTotal: number;
    routeDropNoConn: number;
    routeDropChannelClosed: number;
    routeDropQueueFull: number;
    routeDropQueueFullBase: number;
    routeDropQueueFullHigh: number;
    reconnectAttemptTotal: number;
    reconnectSuccessTotal: number;
  };
  dcRtt: Array<{
    dc: number;
    rttEmaMs?: number | undefined;
    aliveWriters: number;
    requiredWriters: number;
    coveragePct: number;
  }>;
}

// /v1/runtime/me-selftest
export interface ServerSelftestData {
  enabled: boolean;
  kdf: {
    state: string; // ok | error
    ewmaErrorsPerMin: number;
    thresholdErrorsPerMin: number;
    errorsTotal: number;
  };
  timeskew: {
    state: string; // ok | error
    maxSkewSecs15m?: number | undefined;
    samples15m?: number | undefined;
    lastSkewSecs?: number | undefined;
    lastSource?: string | undefined;
  };
  ip: {
    v4?: { addr: string; state: string } | undefined; // good | bogon | loopback
    v6?: { addr: string; state: string } | undefined;
  };
  pid: {
    pid: number;
    state: string; // one | non-one
  };
  bnd: {
    addrState: string; // ok | bogon | error
    portState: string; // ok | zero | error
    lastAddr?: string | undefined;
    lastSeenAgeSecs?: number | undefined;
  };
}

// /v1/runtime/nat_stun
export interface ServerNatStunData {
  enabled: boolean;
  natProbeEnabled: boolean;
  natProbeDisabledRuntime: boolean;
  liveStunTotal: number;
  configuredStunTotal: number;
  configuredServers: string[]; // list of stun server addresses
  reflectionV4?: { addr: string; ageSecs: number } | undefined;
  reflectionV6?: { addr: string; ageSecs: number } | undefined;
  stunBackoffRemainingMs?: number | undefined;
}

// /v1/runtime/events/recent → events[]
export interface ServerEventData {
  seq: number;
  tsEpochSecs: number;
  eventType: string;
  context: string;
}

// /v1/stats/upstreams → zero counters
export interface ServerUpstreamZeroCounters {
  connectAttemptTotal: number;
  connectSuccessTotal: number;
  connectFailTotal: number;
  connectFailfastHardErrorTotal: number;
  connectAttemptsBucket1: number;
  connectAttemptsBucket2: number;
  connectAttemptsBucket3_4: number;
  connectAttemptsBucketGt4: number;
  connectDurationSuccessBucketLe100ms: number;
  connectDurationSuccessBucket101_500ms: number;
  connectDurationSuccessBucket501_1000ms: number;
  connectDurationSuccessBucketGt1000ms: number;
  connectDurationFailBucketLe100ms: number;
  connectDurationFailBucket101_500ms: number;
  connectDurationFailBucket501_1000ms: number;
  connectDurationFailBucketGt1000ms: number;
}

// /v1/stats/minimal/all → network_path[]
export interface ServerNetworkPathData {
  dc: number;
  ipPreference?: string | undefined;
  selectedAddrV4?: string | undefined;
  selectedAddrV6?: string | undefined;
}

// /v1/stats/upstreams → summary
export interface ServerUpstreamSummaryData {
  configuredTotal: number;
  healthyTotal: number;
  unhealthyTotal: number;
  directTotal: number;
  socks4Total: number;
  socks5Total: number;
  shadowsocksTotal: number;
}

export interface ServerDetailPageProps {
  server: {
    id: string;
    name: string;
    ip?: string | undefined;
    status: Status;

    // /v1/system/info
    systemInfo: ServerSystemInfoData;

    // /v1/runtime/gates + /v1/health + /v1/runtime/initialization
    gates: ServerGatesData;

    // /v1/stats/dcs
    dcs: ServerDcData[];

    // /v1/runtime/connections/summary
    connections: ServerConnectionsData;

    // /v1/stats/summary
    summary: ServerSummaryData;

    // /v1/stats/me-writers + /v1/runtime/me_pool_state
    mePool?: ServerMePoolData | undefined;

    // /v1/stats/upstreams
    upstreams: ServerUpstreamData[];
    upstreamSummary?: ServerUpstreamSummaryData | undefined;
    upstreamZeroCounters?: ServerUpstreamZeroCounters | undefined;

    // /v1/runtime/me_quality
    meQuality?: ServerMeQualityData | undefined;

    // /v1/runtime/me-selftest
    selftest?: ServerSelftestData | undefined;

    // /v1/runtime/nat_stun
    natStun?: ServerNatStunData | undefined;

    // /v1/runtime/events/recent
    events: ServerEventData[];
    eventsDroppedTotal: number;

    // /v1/stats/minimal/all → network_path
    networkPath?: ServerNetworkPathData[] | undefined;
  };
  onBack?: (() => void) | undefined;
  onReload?: (() => void) | undefined;
  onBoostDetail?: (() => void) | undefined;
  agentConnection?: AgentConnectionData | undefined;
  initState?: InitCardProps | undefined;
  lastUpdatedAt?: Date | undefined;
  onAllowReEnrollment?: (() => void) | undefined;
  onRevokeGrant?: (() => void) | undefined;
  onRename?: ((name: string) => void) | undefined;
  onDeregister?: (() => void) | undefined;
  metricsChart?: {
    points: import("@/features/dashboard/ui/MetricsChartSection").MetricsPoint[];
    resolution?: "raw" | "hourly" | undefined;
    timeRange: string;
    onTimeRangeChange?: ((range: string) => void) | undefined;
  } | undefined;
}

// --- Clients ---

export interface ClientListItem {
  id: string;
  name: string;
  enabled: boolean;
  assignedNodesCount: number;
  expirationRfc3339: string;
  trafficUsedBytes: number;
  uniqueIpsUsed: number;
  activeTcpConns: number;
  dataQuotaBytes: number;
  lastDeployStatus: string;
}

/**
 * Bulk operations operators can fire against a multi-selection of
 * clients on /clients. Mirrors `BulkServerAction` pattern from the
 * Servers list. Container translates each value into one or more
 * apiClient calls (updateClient with `enabled` flipped, deleteClient
 * per id).
 */
export type BulkClientAction = "enable" | "disable" | "delete";

export interface ClientsPageProps {
  clients: ClientListItem[];
  viewMode: ViewMode;
  autoThreshold: number;
  onViewModeChange?: ((mode: ViewMode) => void) | undefined;
  onClientClick?: ((clientId: string) => void) | undefined;
  onClientLinkClick?: ((clientId: string) => void) | undefined;
  onCreate?: ((data: ClientFormData) => void | Promise<void>) | undefined;
  createLoading?: boolean | undefined;
  createError?: string | undefined;
  /** Options threaded into the create sheet's deployment selectors. */
  fleetGroups?: FleetGroupOption[] | undefined;
  agents?: ClientAgentOption[] | undefined;
  pendingDiscoveredCount?: number | undefined;
  onDiscoveredClick?: (() => void) | undefined;
  /** Bulk action callback. Container wires it to apiClient calls per id. */
  onBulkAction?: ((action: BulkClientAction, clientIds: string[]) => void | Promise<void>) | undefined;
  bulkError?: string | undefined;
  bulkPending?: boolean | undefined;
}

// --- Discovered Clients ---

export interface DiscoveredClientConflict {
  type: "same_secret_different_names" | "same_name_different_secrets";
  relatedIds: string[];
}

export interface DiscoveredClientItem {
  id: string;
  agentId: string;
  nodeName: string;
  clientName: string;
  status: "pending_review" | "adopted" | "ignored";
  totalOctets: number;
  currentConnections: number;
  activeUniqueIps: number;
  links: UserLinks;
  maxTcpConns: number;
  maxUniqueIps: number;
  dataQuotaBytes: number;
  expiration: string;
  discoveredAtUnix: number;
  updatedAtUnix: number;
  conflicts?: DiscoveredClientConflict[] | undefined;
}

export interface DiscoveredClientsPageProps {
  clients: DiscoveredClientItem[];
  onAdopt?: ((id: string) => void) | undefined;
  onIgnore?: ((id: string) => void) | undefined;
  onAdoptMany?: ((ids: string[]) => void) | undefined;
  onIgnoreMany?: ((ids: string[]) => void) | undefined;
  onBack?: (() => void) | undefined;
  busy?: boolean | undefined;
}

export interface ClientDeploymentData {
  agentId: string;
  desiredOperation: string;
  status: string;
  lastError: string;
  links: UserLinks;
  lastAppliedAtUnix: number;
}

export interface ClientDetailPageProps {
  client: {
    id: string;
    name: string;
    enabled: boolean;
    secret: string;
    userAdTag: string;
    trafficUsedBytes: number;
    uniqueIpsUsed: number;
    activeTcpConns: number;
    maxTcpConns: number;
    maxUniqueIps: number;
    dataQuotaBytes: number;
    expirationRfc3339: string;
    fleetGroupIds: string[];
    agentIds: string[];
    deployments: ClientDeploymentData[];
  };
  onBack?: (() => void) | undefined;
  onEdit?: ((data: ClientFormData) => void | Promise<void>) | undefined;
  editLoading?: boolean | undefined;
  editError?: string | undefined;
  /** Options threaded into the edit sheet's deployment selectors. */
  fleetGroups?: FleetGroupOption[] | undefined;
  agents?: ClientAgentOption[] | undefined;
  onManageAccess?: (() => void) | undefined;
  onRotateSecret?: (() => void) | undefined;
  secretRotating?: boolean | undefined;
  secretPendingRedeploy?: boolean | undefined;
  /** Retry the rollout of the current stored state to every target
   *  agent. Surfaced as a button when at least one deployment is in
   *  the `failed` state. Container wires it to apiClient.redeployClient. */
  onRedeploy?: (() => void) | undefined;
  redeploying?: boolean | undefined;
  onDisable?: (() => void) | undefined;
  onDelete?: (() => void) | undefined;
  ipHistory?: {
    /**
     * GeoIP fields are optional — the backend does not enrich yet. The
     * page renders "—" placeholders for the column until the enrichment
     * lands.
     */
    ips: {
      ip: string;
      firstSeen: string;
      lastSeen: string;
      countryCode?: string | undefined;
      countryName?: string | undefined;
      city?: string | undefined;
      asn?: string | undefined;
    }[];
    totalUnique: number;
  } | undefined;
  /**
   * Mapping `agent_id → node_name` so the Deployments & Links card can
   * render human-readable names instead of raw UUIDs. Until the backend
   * starts including node_name on clientDeploymentResponse
   * (backend-followup #5), the container joins client-side against
   * /api/agents. Missing ids fall back to the UUID.
   */
  agentLabels?: Record<string, string> | undefined;
}

// --- Settings ---

export interface SettingsPageProps {
  panelSettings: {
    httpPublicUrl: string;
    grpcPublicEndpoint: string;
  };
  appearanceSettings: {
    theme: "system" | "light" | "dark";
    density: "comfortable" | "compact";
    helpMode: "off" | "basic" | "full";
    swipeNavigation: boolean;
  };
  onPanelSettingsChange?: ((settings: SettingsPageProps["panelSettings"]) => void) | undefined;
  onAppearanceChange?: ((settings: SettingsPageProps["appearanceSettings"]) => void) | undefined;
  onRestart?: (() => void) | undefined;
  onManageUsers?: (() => void) | undefined;
  retentionSettings?: {
    ts_raw_seconds: number;
    ts_hourly_seconds: number;
    ts_dc_seconds: number;
    ip_history_seconds: number;
    event_history_seconds: number;
  } | undefined;
  onRetentionChange?: ((settings: NonNullable<SettingsPageProps["retentionSettings"]>) => void) | undefined;
  /** True while the retention save mutation is in-flight. Disables the Save
   *  button and swaps the label to "Saving…" so the operator sees feedback. */
  retentionSaving?: boolean | undefined;
  /** Content injected into the "Updates" tab (e.g. UpdatesSettingsSection from core/web). */
  children?: React.ReactNode | undefined;
}

// --- Profile ---

export interface TotpSetupData {
  secret: string;
  otpauthUrl: string;
}

export interface ProfilePageProps {
  user: {
    id: string;
    username: string;
    role: string;
    totpEnabled: boolean;
  };
  appearance: SettingsPageProps["appearanceSettings"];
  onAppearanceChange?: ((settings: SettingsPageProps["appearanceSettings"]) => void) | undefined;
  onStartTotpSetup?: (() => Promise<TotpSetupData>) | undefined;
  onEnableTotp?: ((password: string, totpCode: string) => Promise<void>) | undefined;
  onDisableTotp?: ((password: string, totpCode: string) => Promise<void>) | undefined;
  totpSetupLoading?: boolean | undefined;
  totpEnableLoading?: boolean | undefined;
  totpDisableLoading?: boolean | undefined;
  totpError?: string | undefined;
}

// --- Login ---

export interface LoginPageProps {
  /** Called with credentials only on stage 1. If TOTP is required, parent sets stage to "totp". */
  onCredentials: (username: string, password: string) => Promise<void>;
  /** Called on stage 2 with the TOTP code. Parent has already stored username/password. */
  onTotp: (totpCode: string) => Promise<void>;
  /** Called when user clicks "Back" on stage 2 */
  onBack: () => void;
  /** Controls which stage is shown — parent owns this state */
  stage?: "credentials" | "totp" | undefined;
  error?: string | undefined;
  loading?: boolean | undefined;
}

// --- Enrollment ---

export interface FleetGroupOption {
  id: string;
  name?: string | undefined;
  label?: string | undefined;
  nodeCount?: number | undefined;
  agentCount?: number | undefined;
}

export interface EnrollmentWizardProps {
  step: 1 | 2 | 3;
  // Step 1
  fleetGroups: FleetGroupOption[];
  nodeName: string;
  selectedFleetGroup: string;
  tokenTtl: number;
  onNodeNameChange: (name: string) => void;
  onFleetGroupChange: (id: string) => void;
  /** Optional inline-create hook. When provided, the wizard renders
   *  a "+ New group" button next to the select that opens a mini
   *  dialog owned by the container. */
  onCreateFleetGroup?: (() => void) | undefined;
  onTokenTtlChange: (seconds: number) => void;
  onGenerateToken: () => void;
  // Step 2
  installCommand: string;
  tokenValue: string;
  tokenExpiresInSecs: number;
  advancedOptions?: {
    telemtUrl: string;
    telemtMetricsUrl: string;
    telemtAuth: string;
    /** Pass `--insecure-transport` to the bootstrap command. Use only on
     *  trusted private links (VPN-only / internal network) where the panel
     *  runs plain HTTP and TLS is terminated elsewhere or not at all. */
    insecureTransport: boolean;
  } | undefined;
  onAdvancedOptionsChange?: ((opts: {
    telemtUrl: string;
    telemtMetricsUrl: string;
    telemtAuth: string;
    insecureTransport: boolean;
  }) => void) | undefined;
  onInstallConfirm: () => void;
  onBack: () => void;
  // Step 3
  connectionStatus: {
    // All three stages share the same state machine now: pending →
    // waiting (in progress) → done. Bootstrap used to be modelled as a
    // binary flip the moment the operator hit "I've run the command",
    // but the wizard actually waits for the backend to confirm token
    // consumption, so it needs the full progression.
    bootstrap: "pending" | "waiting" | "done";
    grpcConnect: "pending" | "waiting" | "done";
    firstData: "pending" | "waiting" | "done";
  };
  connectedAgent?: {
    id: string;
    version: string;
    fleetGroup: string;
    certExpiresAt: string;
  } | undefined;
  onViewDetails: () => void;
  onCancel: () => void;
  // Shared
  loading?: boolean | undefined;
  error?: string | undefined;
}

export interface EnrollmentTokenData {
  value: string;
  fleetGroupId: string;
  status: "active" | "consumed" | "expired" | "revoked";
  issuedAtUnix: number;
  expiresAtUnix: number;
}

export interface TokenListProps {
  tokens: EnrollmentTokenData[];
  onRevoke: (tokenValue: string) => void;
  /** Snapshot of current unix seconds, used to render TTL countdowns. */
  nowSec?: number | undefined;
}

// --- Agent Connection ---

export interface AgentConnectionData {
  presenceState: "online" | "degraded" | "offline";
  lastSeenAt: string;
  agentId: string;
  version: string;
  fleetGroup: string;
  certificate: {
    issuedAt: string;
    expiresAt: string;
    remainingDays: number;
  };
  recoveryGrant?: {
    status: "allowed" | "used" | "revoked";
    expiresAtUnix: number;
  } | undefined;
  /** Latest available agent version. When set and differs from `version`, shows an update indicator. */
  latestAgentVersion?: string | undefined;
  /** Called when the user clicks the "Update" button. */
  onUpdate?: (() => void) | undefined;
}

export interface AgentConnectionSectionProps {
  data: AgentConnectionData;
  onAllowReEnrollment: () => void;
  onRevokeGrant: () => void;
}

// --- Init State ---

export interface InitCardProps {
  stage: string;
  progressPct: number;
  attempt: number;
  retryLimit: number;
  elapsedSecs: number;
  lastError?: string | undefined;
  degraded?: boolean | undefined;
}

// --- Client Form ---

export interface NodeOption {
  id: string;
  name: string;
  status: Status;
  fleetGroup: string;
}

export interface FleetGroupChipsProps {
  groups: FleetGroupOption[];
  selected: string[];
  onChange: (selected: string[]) => void;
}

export interface NodeSelectorProps {
  nodes: NodeOption[];
  selectedNodeIds: string[];
  onChange: (ids: string[]) => void;
}

export interface ClientFormData {
  name: string;
  userAdTag: string;
  /**
   * Controls the user_ad_tag_auto flag sent to the backend. When
   * true (default for create mode), the control-plane mints a tag
   * automatically if `userAdTag` is empty. When false, the raw
   * `userAdTag` is stored as-is (empty means the client has no tag).
   */
  userAdTagAuto: boolean;
  expirationRfc3339: string;
  maxTcpConns: number;
  maxUniqueIps: number;
  dataQuotaBytes: number;
  fleetGroupIds: string[];
  agentIds: string[];
}

export interface ClientAgentOption {
  id: string;
  nodeName: string;
  fleetGroupId: string;
  online?: boolean | undefined;
}

export interface ClientFormSheetProps {
  mode: "create" | "edit";
  data: ClientFormData;
  onChange: (data: ClientFormData) => void;
  onSubmit: () => void;
  onCancel: () => void;
  loading?: boolean | undefined;
  error?: string | undefined;
  /** Fleet groups available for assignment. Omitted → selector hidden. */
  fleetGroups?: FleetGroupOption[] | undefined;
  /** Agents available for explicit assignment. Omitted → selector hidden. */
  agents?: ClientAgentOption[] | undefined;
}

export interface ClientAccessSheetProps {
  fleetGroups: FleetGroupOption[];
  nodes: NodeOption[];
  selectedFleetGroupIds: string[];
  selectedNodeIds: string[];
  onFleetGroupsChange: (ids: string[]) => void;
  onNodesChange: (ids: string[]) => void;
  onSubmit: () => void;
  onCancel: () => void;
  loading?: boolean | undefined;
}

export interface SecretRevealProps {
  secret: string;
  onDismiss: () => void;
}

// --- User Management ---

export interface UserListItem {
  id: string;
  username: string;
  role: "admin" | "operator" | "viewer";
  totpEnabled: boolean;
  createdAt: string;
}

export interface UsersSectionProps {
  users: UserListItem[];
  onAdd: () => void;
  onEdit: (userId: string) => void;
  onResetTotp: (userId: string) => void;
  onDelete: (userId: string) => void;
}

export interface UserFormData {
  username: string;
  password: string;
  role: "admin" | "operator" | "viewer";
}

export interface UserFormSheetProps {
  mode: "create" | "edit";
  data: UserFormData;
  onChange: (data: UserFormData) => void;
  onSubmit: () => void;
  onCancel: () => void;
  loading?: boolean | undefined;
  error?: string | undefined;
}

export interface UsersManagementPageProps {
  users: UserListItem[];
  onAdd: () => void;
  onEdit: (userId: string) => void;
  onDelete: (userId: string) => void;
  onResetTotp: (userId: string) => void;
  sheet?: UserFormSheetProps | undefined;
}

// --- Enrollment Tokens ---

export interface EnrollmentTokensPageProps {
  tokens: EnrollmentTokenData[];
  onCreateToken: () => void;
  onRevoke: (tokenValue: string) => void;
}

// --- Activity (Jobs + Audit) ---

export interface JobListItem {
  id: string;
  action: string;
  status: string;
  actorId: string;
  /** Resolved human label for actorId (username). Falls back to shortId. */
  actorLabel?: string | undefined;
  targetCount: number;
  createdAtUnix: number;
  /** First failing target's result_text, if any. Shown under the action row. */
  failureReason?: string | undefined;
}

export interface AuditListItem {
  id: string;
  actorId: string;
  /** Resolved human label for actorId (username). */
  actorLabel?: string | undefined;
  action: string;
  targetId: string;
  /** Resolved human label for targetId (client name, node_name, or username). */
  targetLabel?: string | undefined;
  /** Entity kind derived from action namespace ("user", "client", "agent", …). */
  targetKind?: string | undefined;
  createdAtUnix: number;
}

export interface ActivityPageProps {
  jobs: JobListItem[];
  auditEvents: AuditListItem[];
  activeTab: string;
  onTabChange: (tab: string) => void;
  /** Non-fatal warning when actor/target label lookup failed — rows render
   *  with raw UUIDs. Rendered as a banner above the list. */
  lookupError?: string | null;
}
