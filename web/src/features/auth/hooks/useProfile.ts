import { useQuery } from "@tanstack/react-query";
import type { MeResponse } from "@/shared/api/api";
import { apiClient } from "@/shared/api/api";
import { authKeys } from "@/features/auth/queryKeys";

export function useProfile() {
  const query = useQuery({
    queryKey: authKeys.me(),
    queryFn: ({ signal }) => apiClient.me({ signal }),
  });

  const profile: MeResponse | undefined = query.data;

  return { profile, isLoading: query.isLoading, error: query.error };
}
