import { useQuery } from "@tanstack/react-query";
import type { DashboardOverviewData, DashboardTimelineData } from "@panvex/ui";
import { apiClient } from "@/lib/api";
import {
  transformDashboardOverview,
  transformDashboardTimeline,
} from "@/lib/transforms/dashboard";

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

  return { overview, timeline, isLoading: query.isLoading, error: query.error };
}
