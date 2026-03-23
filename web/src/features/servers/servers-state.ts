import { useQuery } from "@tanstack/react-query";
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
