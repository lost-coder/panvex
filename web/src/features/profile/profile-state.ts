import { useQuery, useMutation } from "@tanstack/react-query";
import { apiClient } from "../../lib/api";

export function useAppearanceSettings() {
  return useQuery({
    queryKey: ["appearance-settings"],
    queryFn: () => apiClient.appearanceSettings(),
  });
}

export function useUpdateAppearanceSettings() {
  return useMutation({
    mutationFn: (payload: Parameters<typeof apiClient.updateAppearanceSettings>[0]) =>
      apiClient.updateAppearanceSettings(payload),
  });
}

export function useMe() {
  return useQuery({
    queryKey: ["me"],
    queryFn: () => apiClient.me(),
  });
}
