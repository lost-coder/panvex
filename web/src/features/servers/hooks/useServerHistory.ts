import { useQuery } from "@tanstack/react-query";
import { apiClient } from "@/shared/api/api";
import { telemetryKeys } from "@/features/servers/queryKeys";
import { useEventAwareInterval } from "@/shared/hooks/useEventAwareInterval";

export function useServerLoadHistory(agentID: string, from?: string, to?: string) {
  const refetchInterval = useEventAwareInterval(300_000, 60_000);

  const query = useQuery({
    queryKey: telemetryKeys.serverLoadHistory(agentID, from, to),
    queryFn: ({ signal }) => apiClient.serverLoadHistory(agentID, from, to, { signal }),
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
    queryKey: telemetryKeys.serverDCHistory(agentID, from, to),
    queryFn: ({ signal }) => apiClient.dcHealthHistory(agentID, from, to, { signal }),
    enabled: !!agentID,
    refetchInterval,
  });

  return {
    points: query.data?.points ?? [],
    isLoading: query.isLoading,
  };
}
