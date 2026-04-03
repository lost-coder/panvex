import { useQuery } from "@tanstack/react-query";
import type { ServerListItem } from "@panvex/ui";
import { apiClient } from "@/lib/api";
import { transformServerList } from "@/lib/transforms/servers";

export function useServersList() {
  const query = useQuery({
    queryKey: ["telemetry", "servers"],
    queryFn: () => apiClient.telemetryServers(),
    refetchInterval: 15_000,
  });

  const servers: ServerListItem[] = query.data
    ? transformServerList(query.data)
    : [];

  return { servers, isLoading: query.isLoading, error: query.error };
}
