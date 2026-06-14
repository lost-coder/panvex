import type { Status } from "@/ui/tokens/colors";
import type { NodeState } from "@/ui";
import type { AgentConnectionData, InitCardProps, TransportMode } from "./common";
import type {
  ServerConnectionsData,
  ServerDcData,
  ServerEventData,
  ServerGatesData,
  ServerMePoolData,
  ServerMeQualityData,
  ServerNatStunData,
  ServerNetworkPathData,
  ServerSelftestData,
  ServerSummaryData,
  ServerSystemInfoData,
  ServerUpstreamData,
  ServerUpstreamSummaryData,
  ServerUpstreamZeroCounters,
} from "./server-detail-data";

// --- Server Detail page props ---
//
// Per-endpoint data shapes live in `./server-detail-data.ts` so this
// file stays focused on the page-level prop contract.

export interface ServerDetailPageProps {
  server: {
    id: string;
    name: string;
    ip?: string | undefined;
    status: Status;
    /** Full lifecycle state (offline/down/degraded/pending/ok). */
    state: NodeState;

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

    // --- Direct-mode panel signals ---
    //
    // Mirror the agent-reported mode booleans alongside the persisted
    // transport_mode and fallback_entered_at_unix from the agents row so
    // direct-mode surfaces (mode banner, fallback duration) can render
    // without digging into `gates` for a subset of the fields.
    useMiddleProxy: boolean;
    meRuntimeReady: boolean;
    me2dcFallbackEnabled: boolean;
    transportMode: TransportMode;
    fallbackEnteredAtUnix: number | null;
    /** true only when the agent's telemetry stream reports reachability loss. */
    telemtUnreachable: boolean;
    /** Unix timestamp (seconds) when telemt became unreachable; 0 when unknown. */
    telemtUnreachableSinceUnix: number;
  };
  onBack?: (() => void) | undefined;
  onReload?: (() => void) | undefined;
  /** Restart the node's Telemt process (heavier than reload). */
  onRestart?: (() => void) | undefined;
  onBoostDetail?: (() => void) | undefined;
  agentConnection?: AgentConnectionData | undefined;
  initState?: InitCardProps | undefined;
  lastUpdatedAt?: Date | undefined;
  onAllowReEnrollment?: (() => void) | undefined;
  onRevokeGrant?: (() => void) | undefined;
  onRename?: ((name: string) => void) | undefined;
  /**
   * Reassign this server to a different fleet group. The dialog
   * presents `fleetGroups` as choices and only fires the callback
   * when the selection actually differs from the current group.
   */
  onChangeFleetGroup?: ((fleetGroupId: string) => void) | undefined;
  /** All groups the operator can move the server to. */
  fleetGroups?: import("@/shared/api/api").FleetGroupEntry[] | undefined;
  /** Current fleet-group id of this server (from the live agent record). */
  currentFleetGroupId?: string | undefined;
  onDeregister?: (() => void) | undefined;
  metricsChart?: {
    points: import("@/features/dashboard/ui/MetricsChartSection").MetricsPoint[];
    resolution?: "raw" | "hourly" | undefined;
    timeRange: string;
    onTimeRangeChange?: ((range: string) => void) | undefined;
  } | undefined;
  /**
   * Optional render slot for the enrollment-history block. The owning
   * container supplies a fully-wired `<EnrollmentHistory agentId=… />`
   * here. We pass it as a node — not import the component — so the
   * presentational page can render in unit tests without a QueryClient.
   */
  enrollmentHistorySlot?: import("react").ReactNode;
  /**
   * Optional render slot for the Phase-3 runtime-events block. The
   * container supplies a wired `<RuntimeEvents agentId=… />`; we accept
   * it as a node so this presentational page stays decoupled from the
   * QueryClient + WebSocket dependencies the hook pulls in.
   */
  runtimeEventsSlot?: import("react").ReactNode;
}
