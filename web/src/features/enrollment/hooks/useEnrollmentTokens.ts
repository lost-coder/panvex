import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import type { EnrollmentTokenData } from "@/shared/api/types-pages/pages";
import { apiClient } from "@/shared/api/api";
import { useToast } from "@/app/providers/ToastProvider";

function transformTokens(raw: Awaited<ReturnType<typeof apiClient.listEnrollmentTokens>>): EnrollmentTokenData[] {
  return raw.map((t) => ({
    value: t.value,
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

  const query = useQuery({
    queryKey: ["enrollmentTokens"],
    queryFn: () => apiClient.listEnrollmentTokens(),
    refetchInterval: 30_000,
  });

  const tokens: EnrollmentTokenData[] = query.data ? transformTokens(query.data) : [];

  const createToken = useMutation({
    mutationFn: (payload: { fleet_group_id: string; ttl_seconds: number }) =>
      apiClient.createEnrollmentToken(payload),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["enrollmentTokens"] }),
    onError: (err: Error) => toast.error(err.message),
  });

  const revokeToken = useMutation({
    mutationFn: (value: string) => apiClient.revokeEnrollmentToken(value),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["enrollmentTokens"] }),
    onError: (err: Error) => toast.error(err.message),
  });

  return { tokens, isLoading: query.isLoading, error: query.error, createToken, revokeToken };
}
