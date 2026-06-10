import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { apiClient } from "@/shared/api/api";
import type {
  GeoIPResponseParsed,
  GeoIPSettingsParsed,
} from "@/shared/api/schemas";
import { settingsKeys } from "@/features/settings/queryKeys";
import { useToast } from "@/app/providers/ToastProvider";

export function useGeoIPSettings() {
  const qc = useQueryClient();
  const toast = useToast();
  const { t } = useTranslation("settings");

  const query = useQuery<GeoIPResponseParsed>({
    queryKey: settingsKeys.geoip(),
    queryFn: () => apiClient.getGeoIPSettings(),
  });

  const saveMutation = useMutation({
    mutationFn: (settings: GeoIPSettingsParsed) => apiClient.putGeoIPSettings(settings),
    onSuccess: (data) => {
      qc.setQueryData(settingsKeys.geoip(), data);
      toast.success(t("toasts.geoipSaved"));
    },
    onError: (err: Error) => toast.error(t("toasts.saveFailed", { message: err.message })),
  });

  const refreshMutation = useMutation({
    mutationFn: () => apiClient.refreshGeoIP(),
    onSuccess: (data) => {
      qc.setQueryData(settingsKeys.geoip(), data);
      toast.success(t("toasts.geoipRefreshed"));
    },
    onError: (err: Error) => toast.error(t("toasts.refreshFailed", { message: err.message })),
  });

  return {
    response: query.data ?? null,
    isLoading: query.isLoading,
    save: saveMutation,
    refresh: refreshMutation,
  };
}
