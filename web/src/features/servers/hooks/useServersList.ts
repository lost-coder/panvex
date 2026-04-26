import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import type { ServerListItem } from "@/shared/api/types-pages/pages";
import { apiClient } from "@/shared/api/api";
import { useWsStatus } from "@/app/providers/EventsSynchronizer";
import { transformServerList, extractAgentVersions } from "@/shared/api/transforms/servers";

export function useServersList() {
  // Q3.U-P-05: relax polling when WS is open; WS invalidates the query.
  const ws = useWsStatus();
  const refetchInterval = ws.status === "open" ? 60_000 : 15_000;

  const query = useQuery({
    queryKey: ["telemetry", "servers"],
    queryFn: () => apiClient.telemetryServers(),
    refetchInterval,
  });

  // Q3.U-P-06: memoise derivations on query.data identity.
  const servers: ServerListItem[] = useMemo(
    () => (query.data ? transformServerList(query.data) : []),
    [query.data],
  );

  // Map of server id -> agent version for update comparison
  const agentVersions: Record<string, string> = useMemo(
    () => (query.data ? extractAgentVersions(query.data) : {}),
    [query.data],
  );

  return { servers, agentVersions, isLoading: query.isLoading, error: query.error };
}
