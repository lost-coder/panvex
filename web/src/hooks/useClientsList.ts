import { useQuery } from "@tanstack/react-query";
import type { ClientListItem } from "@panvex/ui";
import { apiClient } from "@/lib/api";
import { transformClientList } from "@/lib/transforms/clients";

export function useClientsList() {
  const query = useQuery({
    queryKey: ["clients"],
    queryFn: () => apiClient.clients(),
    refetchInterval: 15_000,
  });

  const clients: ClientListItem[] = query.data
    ? transformClientList(query.data)
    : [];

  return { clients, isLoading: query.isLoading, error: query.error };
}
