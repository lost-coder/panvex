import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import type { ClientListItem } from "@/shared/api/types-pages/pages";
import { apiClient } from "@/shared/api/api";
import { transformClientList } from "@/shared/api/transforms/clients";
import { clientsKeys } from "@/features/clients/queryKeys";
import { useEventAwareInterval } from "@/shared/hooks/useEventAwareInterval";

export function useClientsList() {
  const refetchInterval = useEventAwareInterval(60_000, 15_000);

  const query = useQuery({
    queryKey: clientsKeys.list(),
    queryFn: ({ signal }) => apiClient.clients({ signal }),
    refetchInterval,
  });

  // Q3.U-P-06: memoise derivations on query.data identity (образец —
  // useServersList/useDashboardData; #web-2).
  const clients: ClientListItem[] = useMemo(
    () => (query.data ? transformClientList(query.data) : []),
    [query.data],
  );

  return { clients, isLoading: query.isLoading, error: query.error, refetch: query.refetch };
}
