import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { apiClient, type RetentionSettings } from "@/lib/api";

export function useRetentionSettings() {
  const qc = useQueryClient();

  const query = useQuery({
    queryKey: ["settings", "retention"],
    queryFn: () => apiClient.getRetentionSettings(),
  });

  const saveMutation = useMutation({
    mutationFn: (settings: RetentionSettings) => apiClient.putRetentionSettings(settings),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["settings", "retention"] }),
    onError: (err) => console.error("Failed to save retention settings:", err),
  });

  return {
    retention: query.data ?? null,
    isLoading: query.isLoading,
    save: saveMutation,
  };
}
