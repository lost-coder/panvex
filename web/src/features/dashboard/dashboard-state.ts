import { useQuery } from "@tanstack/react-query";
import { apiClient } from "../../lib/api";
export { useTelemetryDashboard as useDashboardData } from "../telemetry/telemetry-state";
export { useTelemetryServers as useAgentsList } from "../telemetry/telemetry-state";

export function useDashboardClients() {
  return useQuery({
    queryKey: ["clients"],
    queryFn: () => apiClient.clients(),
  });
}
