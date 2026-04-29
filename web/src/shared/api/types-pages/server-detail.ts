import type { Status } from "@/ui/tokens/colors";
import type { AgentConnectionData, InitCardProps } from "./common";
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
}
