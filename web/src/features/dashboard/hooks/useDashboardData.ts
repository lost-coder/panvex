import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import type { DashboardOverviewData, DashboardTimelineData } from "@/shared/api/types-pages/pages";
import { apiClient } from "@/shared/api/api";
import { useWsStatus } from "@/app/providers/EventsSynchronizer";
import { dashboardKeys } from "@/features/dashboard/queryKeys";
import {
  transformDashboardOverview,
  transformDashboardTimeline,
  extractDashboardAgentVersions,
} from "@/shared/api/transforms/dashboard";

export function useDashboardData() {
  // Q3.U-P-05: when the WS feed is healthy, fall back to a much slower
  // polling cadence — invalidations arrive over the socket. If the WS
  // is closed/reconnecting, keep the legacy 15s pace as a safety net.
  const ws = useWsStatus();
  const refetchInterval = ws.status === "open" ? 60_000 : 15_000;

  const query = useQuery({
    queryKey: dashboardKeys.overview(),
    queryFn: () => apiClient.telemetryDashboard(),
    refetchInterval,
  });

  // Q3.U-P-06: memoise the derived shapes on query.data identity so
  // unrelated parent re-renders do not re-run the heavy transforms or
  // produce new object identities that defeat React.memo downstream.
  const overview: DashboardOverviewData | undefined = useMemo(
    () => (query.data ? transformDashboardOverview(query.data) : undefined),
    [query.data],
  );

  const timeline: DashboardTimelineData | undefined = useMemo(
    () => (query.data ? transformDashboardTimeline(query.data) : undefined),
    [query.data],
  );

  // Map of node id -> agent version for update comparison
  const agentVersions: Record<string, string> = useMemo(
    () => (query.data ? extractDashboardAgentVersions(query.data) : {}),
    [query.data],
  );

  return { overview, timeline, agentVersions, isLoading: query.isLoading, error: query.error };
}
