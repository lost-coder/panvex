import { useQuery } from "@tanstack/react-query";
import { apiClient } from "@/shared/api/api";
import { useEventAwareInterval } from "@/shared/hooks/useEventAwareInterval";

export function useServerLoadHistory(agentID: string, from?: string, to?: string) {
  const refetchInterval = useEventAwareInterval(300_000, 60_000);

  const query = useQuery({
    queryKey: ["telemetry", "server", agentID, "history", "load", from, to],
    queryFn: () => apiClient.serverLoadHistory(agentID, from, to),
    enabled: !!agentID,
    refetchInterval,
  });

  return {
    points: query.data?.points ?? [],
    resolution: query.data?.resolution ?? "raw",
    isLoading: query.isLoading,
  };
}

export function useDCHealthHistory(agentID: string, from?: string, to?: string) {
  const refetchInterval = useEventAwareInterval(300_000, 60_000);

  const query = useQuery({
    queryKey: ["telemetry", "server", agentID, "history", "dc", from, to],
    queryFn: () => apiClient.dcHealthHistory(agentID, from, to),
    enabled: !!agentID,
    refetchInterval,
  });

  return {
    points: query.data?.points ?? [],
    isLoading: query.isLoading,
  };
}
