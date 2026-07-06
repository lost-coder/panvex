import { useQuery } from "@tanstack/react-query";
import { apiClient } from "@/shared/api/api";
import { clientsKeys } from "@/features/clients/queryKeys";
import { useEventAwareInterval } from "@/shared/hooks/useEventAwareInterval";

export function useClientIPHistory(clientID: string) {
  const refetchInterval = useEventAwareInterval(300_000, 60_000);

  const query = useQuery({
    queryKey: clientsKeys.ipHistory(clientID),
    queryFn: ({ signal }) => apiClient.clientIPHistory(clientID, undefined, undefined, { signal }),
    enabled: !!clientID,
    refetchInterval,
  });

  return {
    ips: (query.data?.ips ?? []).map((ip) => ({
      ip: ip.ip_address,
      firstSeen: ip.first_seen,
      lastSeen: ip.last_seen,
      countryCode: ip.country_code,
      countryName: ip.country_name,
      city: ip.city,
      asn: ip.asn,
    })),
    totalUnique: query.data?.total_unique ?? 0,
    // M7: surfaces whether totalUnique above is a real count or a 0
    // placeholder from a failed backend count query. Defaults to true
    // so pre-fix / older responses that omit the field behave exactly
    // as before.
    totalUniqueAvailable: query.data?.total_unique_available ?? true,
    isLoading: query.isLoading,
  };
}
