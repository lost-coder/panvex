import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { apiClient } from "../../lib/api";

export function useServers() {
  return useQuery({
    queryKey: ["agents"],
    queryFn: () => apiClient.agents(),
    refetchInterval: 15_000,
  });
}

export function useServerDetail(agentId: string) {
  const { data: agents = [] } = useServers();
  return agents.find(a => a.id === agentId) ?? null;
}

export function useAllowAgentCertificateRecovery() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: ({ agentID, ttlSeconds = 900 }: { agentID: string; ttlSeconds?: number }) =>
      apiClient.allowAgentCertificateRecovery(agentID, { ttl_seconds: ttlSeconds }),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["agents"] });
    },
  });
}

export function useRevokeAgentCertificateRecovery() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: ({ agentID }: { agentID: string }) =>
      apiClient.revokeAgentCertificateRecovery(agentID),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["agents"] });
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
  };
}
