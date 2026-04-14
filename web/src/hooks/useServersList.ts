import { useQuery } from "@tanstack/react-query";
import type { ServerListItem } from "@lost-coder/panvex-ui";
import { apiClient } from "@/lib/api";
import { transformServerList, extractAgentVersions } from "@/lib/transforms/servers";

export function useServersList() {
  const query = useQuery({
    queryKey: ["telemetry", "servers"],
    queryFn: () => apiClient.telemetryServers(),
    refetchInterval: 15_000,
  });

  const servers: ServerListItem[] = query.data
    ? transformServerList(query.data)
    : [];

  // Map of server id -> agent version for update comparison
  const agentVersions: Record<string, string> = query.data
    ? extractAgentVersions(query.data)
    : {};

  return { servers, agentVersions, isLoading: query.isLoading, error: query.error };
}
