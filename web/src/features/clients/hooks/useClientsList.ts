import { useQuery } from "@tanstack/react-query";
import type { ClientListItem } from "@/shared/api/types-pages/pages";
import { apiClient } from "@/shared/api/api";
import { transformClientList } from "@/shared/api/transforms/clients";
import { useEventAwareInterval } from "@/shared/hooks/useEventAwareInterval";

export function useClientsList() {
  const refetchInterval = useEventAwareInterval(60_000, 15_000);

  const query = useQuery({
    queryKey: ["clients"],
    queryFn: () => apiClient.clients(),
    refetchInterval,
  });

  const clients: ClientListItem[] = query.data
    ? transformClientList(query.data)
    : [];

  return { clients, isLoading: query.isLoading, error: query.error };
}
