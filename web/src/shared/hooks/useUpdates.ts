import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { apiClient, isActiveSelfUpdatePhase } from "@/shared/api/api";
import type { UpdateSettingsResponse } from "@/shared/api/api";
import { updatesKeys } from "@/features/updates/queryKeys";
import { useEventAwareInterval } from "@/shared/hooks/useEventAwareInterval";

// While a self-update is in-flight, poll aggressively so the phase (and its
// terminal outcome) shows up within a few seconds rather than waiting for the
// slow event-aware cadence.
const SELF_UPDATE_ACTIVE_POLL_MS = 3_000;

export function useUpdates() {
  const queryClient = useQueryClient();
  const eventAwareInterval = useEventAwareInterval(300_000, 60_000);

  const query = useQuery({
    queryKey: updatesKeys.settings(),
    queryFn: ({ signal }) => apiClient.getUpdateSettings({ signal }),
    // Function-form refetchInterval: TanStack Query passes the current query,
    // so we can read the just-fetched phase and switch to the fast cadence
    // while an update is active, falling back to the event-aware interval when
    // idle or terminal.
    refetchInterval: (q) => {
      const data = q.state.data as UpdateSettingsResponse | undefined;
      const phase = data?.self_update.phase;
      if (phase && isActiveSelfUpdatePhase(phase)) {
        return SELF_UPDATE_ACTIVE_POLL_MS;
      }
      return eventAwareInterval;
    },
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
