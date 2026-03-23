import { useQuery, useMutation } from "@tanstack/react-query";
import { apiClient } from "../../lib/api";

export function usePanelSettings() {
  return useQuery({
    queryKey: ["panel-settings"],
    queryFn: () => apiClient.panelSettings(),
  });
}

export function useUpdatePanelSettings() {
  return useMutation({
    mutationFn: (payload: Parameters<typeof apiClient.updatePanelSettings>[0]) =>
      apiClient.updatePanelSettings(payload),
  });
}

export function useUsers() {
  return useQuery({
    queryKey: ["users"],
    queryFn: () => apiClient.users(),
  });
}
