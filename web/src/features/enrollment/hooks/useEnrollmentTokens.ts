import { useMemo } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import type { EnrollmentTokenData } from "@/shared/api/types-pages/pages";
import { apiClient } from "@/shared/api/api";
import { enrollmentTokensKeys } from "@/features/enrollment/queryKeys";
import { useToast } from "@/app/providers/ToastProvider";
import { useEventAwareInterval } from "@/shared/hooks/useEventAwareInterval";

function transformTokens(raw: Awaited<ReturnType<typeof apiClient.listEnrollmentTokens>>): EnrollmentTokenData[] {
  return raw.map((t) => ({
    // Listings return masked_value + handle; raw `value` only appears on
    // the creation response. Fall back gracefully so either shape renders.
    handle: t.handle ?? t.value ?? "",
    maskedValue: t.masked_value ?? t.value ?? "",
    fleetGroupId: t.fleet_group_id || "default",
    status: t.status,
    issuedAtUnix: t.issued_at_unix,
    expiresAtUnix: t.expires_at_unix,
  }));
}

export function useEnrollmentTokens() {
  const qc = useQueryClient();
  // Mutations are fire-and-forget from the container. Without an onError
  // toast a failed create/revoke would manifest as "the button did nothing"
  // — React Query would log to console, invisible to the operator. Mirrors
  // the useClientMutations pattern.
  const toast = useToast();
  const refetchInterval = useEventAwareInterval(90_000, 30_000);

  const query = useQuery({
    queryKey: enrollmentTokensKeys.list(),
    queryFn: () => apiClient.listEnrollmentTokens(),
    refetchInterval,
  });

  // Q3.U-P-06: memoise derivations on query.data identity (#web-2).
  const tokens: EnrollmentTokenData[] = useMemo(
    () => (query.data ? transformTokens(query.data) : []),
    [query.data],
  );

  const createToken = useMutation({
    mutationFn: (payload: { fleet_group_id: string; ttl_seconds: number }) =>
      apiClient.createEnrollmentToken(payload),
    onSuccess: () => qc.invalidateQueries({ queryKey: enrollmentTokensKeys.all }),
    onError: (err: Error) => toast.error(err.message),
  });

  const revokeToken = useMutation({
    mutationFn: (value: string) => apiClient.revokeEnrollmentToken(value),
    onSuccess: () => qc.invalidateQueries({ queryKey: enrollmentTokensKeys.all }),
    onError: (err: Error) => toast.error(err.message),
  });

  return { tokens, isLoading: query.isLoading, error: query.error, refetch: query.refetch, createToken, revokeToken };
}
