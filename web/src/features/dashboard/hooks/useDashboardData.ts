import { useQuery } from "@tanstack/react-query";
import type { DashboardOverviewData, DashboardTimelineData } from "@/shared/api/types-pages/pages";
import { apiClient } from "@/shared/api/api";
import {
  transformDashboardOverview,
  transformDashboardTimeline,
  extractDashboardAgentVersions,
} from "@/shared/api/transforms/dashboard";

export function useDashboardData() {
  const query = useQuery({
    queryKey: ["telemetry", "dashboard"],
    queryFn: () => apiClient.telemetryDashboard(),
    refetchInterval: 15_000,
  });

  const overview: DashboardOverviewData | undefined = query.data
    ? transformDashboardOverview(query.data)
    : undefined;

  const timeline: DashboardTimelineData | undefined = query.data
    ? transformDashboardTimeline(query.data)
    : undefined;

  // Map of node id -> agent version for update comparison
  const agentVersions: Record<string, string> = query.data
    ? extractDashboardAgentVersions(query.data)
    : {};

  return { overview, timeline, agentVersions, isLoading: query.isLoading, error: query.error };
}
