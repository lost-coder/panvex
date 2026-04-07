import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import type { EnrollmentTokenData } from "@panvex/ui";
import { apiClient } from "@/lib/api";

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
  });

  const revokeToken = useMutation({
    mutationFn: (value: string) => apiClient.revokeEnrollmentToken(value),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["enrollmentTokens"] }),
  });

  return { tokens, isLoading: query.isLoading, createToken, revokeToken };
}
