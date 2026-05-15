// Server detail response data shapes — one interface per
// `/v1/...` endpoint that the Server Detail page consumes. Kept
// separate from `server-detail.ts` (which owns the page props
// composition) so neither file balloons past the audit threshold.

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
  // Per-class breakdown of bad connections and handshake failures.
  // Telemt 3.4.10 surfaces these on /v1/stats/summary; older Telemt
  // builds emit an empty array (or omit the field, which the schema
  // normalises to `[]`). The class string set is open — see
  // ConnectionClassCount on the Go side.
  connectionsBadByClass: ConnectionClassCount[];
  handshakeFailuresByClass: ConnectionClassCount[];
}

export interface ConnectionClassCount {
  class: string;
  total: number;
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
  // Direct-mode panel: 5-minute fail-rate signal for upstream connect
  // attempts and lifetime connect counters. `failRateKnown` distinguishes
  // "0% because no traffic yet" from "0% because all attempts succeeded".
  failRatePct5m: number; // 0 when unknown
  failRateKnown: boolean;
  connectAttemptTotal: number;
  connectSuccessTotal: number;
  connectFailTotal: number;
  connectFailfastTotal: number;
}
