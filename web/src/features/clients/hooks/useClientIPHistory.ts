import { useQuery } from "@tanstack/react-query";
import { apiClient } from "@/shared/api/api";

export function useClientIPHistory(clientID: string) {
  const query = useQuery({
    queryKey: ["client", clientID, "ip-history"],
    queryFn: () => apiClient.clientIPHistory(clientID),
    enabled: !!clientID,
    refetchInterval: 60_000,
  });

  return {
    ips: (query.data?.ips ?? []).map((ip) => ({
      ip: ip.ip_address,
      firstSeen: ip.first_seen,
      lastSeen: ip.last_seen,
    })),
    totalUnique: query.data?.total_unique ?? 0,
    isLoading: query.isLoading,
  };
}
