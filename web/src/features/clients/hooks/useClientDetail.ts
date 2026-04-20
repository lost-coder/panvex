import { useQuery } from "@tanstack/react-query";
import type { ClientDetailPageProps } from "@/shared/api/types-pages/pages";
import { apiClient } from "@/shared/api/api";
import { transformClientDetail } from "@/shared/api/transforms/clients";

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

  return { client, raw: query.data, isLoading: query.isLoading, error: query.error };
}
