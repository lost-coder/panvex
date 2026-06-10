import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import type { ServerDetailPageProps, InitCardProps } from "@/shared/api/types-pages/pages";
import { apiClient } from "@/shared/api/api";
import { useWsStatus } from "@/app/providers/EventsSynchronizer";
import { telemetryKeys } from "@/features/servers/queryKeys";
import { transformServerDetail, transformInitState } from "@/shared/api/transforms/servers";

export function useServerDetail(serverId: string) {
  // Q3.U-P-05: relax polling when WS is open; WS invalidates the query.
  const ws = useWsStatus();
  const refetchInterval = ws.status === "open" ? 30_000 : 10_000;

  const query = useQuery({
    queryKey: telemetryKeys.server(serverId),
    queryFn: () => apiClient.telemetryServer(serverId),
    refetchInterval,
    enabled: !!serverId,
  });

  // Q3.U-P-06: memoise derivations on query.data identity.
  const server: ServerDetailPageProps["server"] | undefined = useMemo(
    () => (query.data ? transformServerDetail(query.data) : undefined),
    [query.data],
  );

  const initState: InitCardProps | undefined = useMemo(
    () => (query.data ? transformInitState(query.data) : undefined),
    [query.data],
  );

  const lastUpdatedAt = query.dataUpdatedAt ? new Date(query.dataUpdatedAt) : undefined;

  return { server, initState, lastUpdatedAt, raw: query.data, isLoading: query.isLoading, error: query.error, refetch: query.refetch };
}
