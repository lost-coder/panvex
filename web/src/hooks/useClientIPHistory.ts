import { useQuery } from "@tanstack/react-query";
import { apiClient } from "@/lib/api";

export function useClientIPHistory(clientID: string) {
  const query = useQuery({
    queryKey: ["client", clientID, "ip-history"],
    queryFn: () => apiClient.clientIPHistory(clientID),
    enabled: !!clientID,
    refetchInterval: 60_000,
  });

  return {
    ips: (query.data?.ips ?? []).map((ip) => ({
      agentId: ip.AgentID,
      ip: ip.IPAddress,
      firstSeen: ip.FirstSeen,
      lastSeen: ip.LastSeen,
    })),
    totalUnique: query.data?.total_unique ?? 0,
    isLoading: query.isLoading,
  };
}
