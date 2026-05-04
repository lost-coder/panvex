import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
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

  const query = useQuery<GeoIPResponseParsed>({
    queryKey: settingsKeys.geoip(),
    queryFn: () => apiClient.getGeoIPSettings(),
  });

  const saveMutation = useMutation({
    mutationFn: (settings: GeoIPSettingsParsed) => apiClient.putGeoIPSettings(settings),
    onSuccess: (data) => {
      qc.setQueryData(settingsKeys.geoip(), data);
      toast.success("GeoIP settings saved.");
    },
    onError: (err: Error) => toast.error(`Save failed: ${err.message}`),
  });

  const refreshMutation = useMutation({
    mutationFn: () => apiClient.refreshGeoIP(),
    onSuccess: (data) => {
      qc.setQueryData(settingsKeys.geoip(), data);
      toast.success("GeoIP databases refreshed.");
    },
    onError: (err: Error) => toast.error(`Refresh failed: ${err.message}`),
  });

  return {
    response: query.data ?? null,
    isLoading: query.isLoading,
    save: saveMutation,
    refresh: refreshMutation,
  };
}
