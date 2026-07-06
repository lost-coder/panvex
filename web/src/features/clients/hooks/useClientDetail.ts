import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import type { ClientDetailPageProps } from "@/shared/api/types-pages/pages";
import { apiClient } from "@/shared/api/api";
import { transformClientDetail } from "@/shared/api/transforms/clients";
import { clientsKeys } from "@/features/clients/queryKeys";
import { useEventAwareInterval } from "@/shared/hooks/useEventAwareInterval";

export function useClientDetail(clientId: string) {
  const refetchInterval = useEventAwareInterval(60_000, 10_000);

  const query = useQuery({
    queryKey: clientsKeys.detail(clientId),
    queryFn: () => apiClient.client(clientId),
    refetchInterval,
    enabled: !!clientId,
  });

  // Q3.U-P-06: memoise derivations on query.data identity (#web-2).
  const client: ClientDetailPageProps["client"] | undefined = useMemo(
    () => (query.data ? transformClientDetail(query.data) : undefined),
    [query.data],
  );

  return { client, raw: query.data, isLoading: query.isLoading, error: query.error };
}
