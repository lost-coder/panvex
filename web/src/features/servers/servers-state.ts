import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { apiClient } from "../../lib/api";
export { useActivateTelemetryDetailBoost, useRefreshTelemetryDiagnostics, useTelemetryServerDetail as useServerDetail, useTelemetryServers as useServers } from "../telemetry/telemetry-state";
import { invalidateTelemetryQueries } from "../telemetry/telemetry-query-invalidation";

export function useAllowAgentCertificateRecovery() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: ({ agentID, ttlSeconds = 900 }: { agentID: string; ttlSeconds?: number }) =>
      apiClient.allowAgentCertificateRecovery(agentID, { ttl_seconds: ttlSeconds }),
    onSuccess: async (_payload, variables) => {
      await invalidateTelemetryQueries(queryClient, variables.agentID);
    },
  });
}

export function useRevokeAgentCertificateRecovery() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: ({ agentID }: { agentID: string }) =>
      apiClient.revokeAgentCertificateRecovery(agentID),
    onSuccess: async (_payload, variables) => {
      await invalidateTelemetryQueries(queryClient, variables.agentID);
    },
  });
}

export function useServerRecoveryAccess() {
  const { data: me } = useQuery({
    queryKey: ["me"],
    queryFn: () => apiClient.me(),
  });

  return {
    canManageRecovery: me?.role === "admin",
    canRefreshDiagnostics: me?.role === "admin" || me?.role === "operator",
  };
}
