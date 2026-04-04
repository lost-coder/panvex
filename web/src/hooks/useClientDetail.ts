import { useQuery } from "@tanstack/react-query";
import type { ClientDetailPageProps } from "@panvex/ui";
import { apiClient } from "@/lib/api";
import { transformClientDetail } from "@/lib/transforms/clients";

export function useClientDetail(clientId: string) {
  const query = useQuery({
    queryKey: ["client", clientId],
    queryFn: () => apiClient.client(clientId),
    refetchInterval: 10_000,
    enabled: !!clientId,
  });

  const client: ClientDetailPageProps["client"] | undefined = query.data
    ? transformClientDetail(query.data)
    : undefined;

  return { client, isLoading: query.isLoading, error: query.error };
}
