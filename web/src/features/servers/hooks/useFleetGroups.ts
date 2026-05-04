import { useQuery } from "@tanstack/react-query";
import { apiClient } from "@/shared/api/api";
import { fleetGroupsKeys } from "@/features/servers/queryKeys";
import { useEventAwareInterval } from "@/shared/hooks/useEventAwareInterval";

export function useFleetGroups() {
  const refetchInterval = useEventAwareInterval(90_000, 30_000);

  const query = useQuery({
    queryKey: fleetGroupsKeys.list(),
    queryFn: () => apiClient.fleetGroups(),
    refetchInterval,
  });

  return { fleetGroups: query.data ?? [], isLoading: query.isLoading };
}
