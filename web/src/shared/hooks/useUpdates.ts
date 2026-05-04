import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { apiClient } from "@/shared/api/api";
import { updatesKeys } from "@/features/updates/queryKeys";
import { useEventAwareInterval } from "@/shared/hooks/useEventAwareInterval";

export function useUpdates() {
  const queryClient = useQueryClient();
  const refetchInterval = useEventAwareInterval(300_000, 60_000);

  const query = useQuery({
    queryKey: updatesKeys.settings(),
    queryFn: () => apiClient.getUpdateSettings(),
    refetchInterval,
  });

  const saveSettings = useMutation({
    mutationFn: apiClient.putUpdateSettings,
    onSuccess: () => queryClient.invalidateQueries({ queryKey: updatesKeys.all }),
  });

  const checkNow = useMutation({
    mutationFn: () => apiClient.checkForUpdates(),
    onSuccess: () => {
      setTimeout(
        () => queryClient.invalidateQueries({ queryKey: updatesKeys.all }),
        3000
      );
    },
  });

  const updatePanel = useMutation({
    mutationFn: (targetVersion: string) => apiClient.updatePanel(targetVersion),
  });

  return { query, saveSettings, checkNow, updatePanel };
}
