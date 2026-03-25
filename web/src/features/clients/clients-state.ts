import { useQuery } from "@tanstack/react-query";
import { apiClient } from "../../lib/api";

export function useClients() {
  return useQuery({
    queryKey: ["clients"],
    queryFn: () => apiClient.clients(),
    refetchInterval: 15_000,
  });
}

export function useClientDetail(clientId: string) {
  return useQuery({
    queryKey: ["client", clientId],
    queryFn: () => apiClient.client(clientId),
    enabled: !!clientId,
  });
}
