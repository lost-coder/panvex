import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { apiClient, type RetentionSettings } from "@/shared/api/api";
import { settingsKeys } from "@/features/settings/queryKeys";
import { useToast } from "@/app/providers/ToastProvider";

export function useRetentionSettings() {
  const qc = useQueryClient();
  const toast = useToast();
  const { t } = useTranslation("settings");

  const query = useQuery({
    queryKey: settingsKeys.retention(),
    queryFn: () => apiClient.getRetentionSettings(),
  });

  const saveMutation = useMutation({
    mutationFn: (settings: RetentionSettings) => apiClient.putRetentionSettings(settings),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: settingsKeys.retention() });
      toast.success(t("toasts.retentionSaved"));
    },
    onError: (err: Error) => toast.error(t("toasts.saveFailed", { message: err.message })),
  });

  return {
    retention: query.data ?? null,
    isLoading: query.isLoading,
    save: saveMutation,
  };
}
