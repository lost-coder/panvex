import type { Status } from "@/ui/tokens/colors";
import type { AlertData, KpiItem, TimelineEventData, TrendItem } from "./common";
import type { ClientFormData } from "./client-form";

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
