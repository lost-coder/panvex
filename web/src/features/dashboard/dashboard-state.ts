import { useQuery } from "@tanstack/react-query";
import { apiClient } from "../../lib/api";

export function useDashboardData() {
  return useQuery({
    queryKey: ["control-room"],
    queryFn: () => apiClient.controlRoom(),
    refetchInterval: 15_000,
  });
}

export function useAgentsList() {
  return useQuery({
    queryKey: ["agents"],
    queryFn: () => apiClient.agents(),
  });
}
