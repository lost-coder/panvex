import type { Status } from "@/ui/tokens/colors";
import type { FleetGroupOption, Severity, ViewMode } from "./common";

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
  // --- Direct-mode panel signals ---
  //
  // Mirror the agent-reported mode booleans + the unified telemetry
  // severity + upstream health counts so the Servers list can render the
  // mode-aware Transport badge without a second roundtrip.
  useMiddleProxy: boolean;
  meRuntimeReady: boolean;
  me2dcFallbackEnabled: boolean;
  healthyUpstreams: number;
  totalUpstreams: number;
  /**
   * Healthy / total DC count derived from the agent's per-DC coverage
   * report. The Transport column renders these for ME-mode nodes
   * because "upstreams" only counts configured proxy upstreams (often
   * 1 in middle-proxy mode), which is misleading for fleet operators.
   */
  healthyDcs: number;
  totalDcs: number;
  severity: Severity;
  /** false only when the agent's telemetry stream reports reachability loss. */
  telemtReachable: boolean;
  /** Unix timestamp (seconds) when telemt became unreachable; 0 when unknown. */
  telemtUnreachableSinceUnix: number;
}

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
