import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { apiClient } from "@/lib/api";
import { invalidateTelemetryQueries } from "./telemetry-query-invalidation";

export function useTelemetryDashboard() {
  return useQuery({
    queryKey: ["telemetry-dashboard"],
    queryFn: () => apiClient.telemetryDashboard(),
    refetchInterval: 15_000,
  });
}

export function useTelemetryServers() {
  return useQuery({
    queryKey: ["telemetry-servers"],
    queryFn: () => apiClient.telemetryServers(),
    refetchInterval: 15_000,
  });
}

export function useTelemetryServerDetail(agentId: string) {
  return useQuery({
    queryKey: ["telemetry-server", agentId],
    queryFn: () => apiClient.telemetryServer(agentId),
    enabled: !!agentId,
    refetchInterval: 15_000,
  });
}

export function useActivateTelemetryDetailBoost() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: ({ agentID }: { agentID: string }) => apiClient.activateTelemetryDetailBoost(agentID),
    onSuccess: async (_payload, variables) => {
      await invalidateTelemetryQueries(queryClient, variables.agentID);
    },
  });
}

export function useRefreshTelemetryDiagnostics() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: ({ agentID }: { agentID: string }) => apiClient.refreshTelemetryDiagnostics(agentID),
    onSuccess: async (_payload, variables) => {
      await invalidateTelemetryQueries(queryClient, variables.agentID);
    },
  });
}
