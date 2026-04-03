import { useQuery } from "@tanstack/react-query";
import type { ServerDetailPageProps, InitCardProps } from "@panvex/ui";
import { apiClient } from "@/lib/api";
import { transformServerDetail, transformInitState } from "@/lib/transforms/servers";

export function useServerDetail(serverId: string) {
  const query = useQuery({
    queryKey: ["telemetry", "server", serverId],
    queryFn: () => apiClient.telemetryServer(serverId),
    refetchInterval: 10_000,
    enabled: !!serverId,
  });

  const server: ServerDetailPageProps["server"] | undefined = query.data
    ? transformServerDetail(query.data)
    : undefined;

  const initState: InitCardProps | undefined = query.data
    ? transformInitState(query.data)
    : undefined;

  return { server, initState, isLoading: query.isLoading, error: query.error };
}
