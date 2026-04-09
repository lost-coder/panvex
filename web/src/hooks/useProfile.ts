import { useQuery } from "@tanstack/react-query";
import type { MeResponse } from "@/lib/api";
import { apiClient } from "@/lib/api";

export function useProfile() {
  const query = useQuery({
    queryKey: ["me"],
    queryFn: () => apiClient.me(),
  });

  const profile: MeResponse | undefined = query.data;

  return { profile, isLoading: query.isLoading, error: query.error };
}
