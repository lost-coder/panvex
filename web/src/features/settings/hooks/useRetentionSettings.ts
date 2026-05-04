import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { apiClient, type RetentionSettings } from "@/shared/api/api";
import { settingsKeys } from "@/features/settings/queryKeys";
import { useToast } from "@/app/providers/ToastProvider";

export function useRetentionSettings() {
  const qc = useQueryClient();
  const toast = useToast();

  const query = useQuery({
    queryKey: settingsKeys.retention(),
    queryFn: () => apiClient.getRetentionSettings(),
  });

  const saveMutation = useMutation({
    mutationFn: (settings: RetentionSettings) => apiClient.putRetentionSettings(settings),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: settingsKeys.retention() });
      toast.success("Retention settings saved.");
    },
    onError: (err: Error) => toast.error(`Save failed: ${err.message}`),
  });

  return {
    retention: query.data ?? null,
    isLoading: query.isLoading,
    save: saveMutation,
  };
}
