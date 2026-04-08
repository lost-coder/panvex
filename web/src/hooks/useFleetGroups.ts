import { useQuery } from "@tanstack/react-query";
import { apiClient } from "@/lib/api";

export function useFleetGroups() {
  const query = useQuery({
    queryKey: ["fleet-groups"],
    queryFn: () => apiClient.fleetGroups(),
    refetchInterval: 30_000,
  });

  return { fleetGroups: query.data ?? [], isLoading: query.isLoading };
}
