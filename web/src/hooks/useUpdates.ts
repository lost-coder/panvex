import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { apiClient } from "@/lib/api";

export function useUpdates() {
  const queryClient = useQueryClient();

  const query = useQuery({
    queryKey: ["updates"],
    queryFn: () => apiClient.getUpdateSettings(),
    refetchInterval: 60_000,
  });

  const saveSettings = useMutation({
    mutationFn: apiClient.putUpdateSettings,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["updates"] }),
  });

  const checkNow = useMutation({
    mutationFn: () => apiClient.checkForUpdates(),
    onSuccess: () => {
      setTimeout(
        () => queryClient.invalidateQueries({ queryKey: ["updates"] }),
        3000
      );
    },
  });

  const updatePanel = useMutation({
    mutationFn: (version?: string) => apiClient.updatePanel(version),
  });

  return { query, saveSettings, checkNow, updatePanel };
}
