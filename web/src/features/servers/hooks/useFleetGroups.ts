import { useQuery } from "@tanstack/react-query";
import { apiClient } from "@/shared/api/api";
import { useEventAwareInterval } from "@/shared/hooks/useEventAwareInterval";

export function useFleetGroups() {
  const refetchInterval = useEventAwareInterval(90_000, 30_000);

  const query = useQuery({
    queryKey: ["fleet-groups"],
    queryFn: () => apiClient.fleetGroups(),
    refetchInterval,
  });

  return { fleetGroups: query.data ?? [], isLoading: query.isLoading };
}
