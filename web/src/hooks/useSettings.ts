import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import type { SettingsPageProps } from "@panvex/ui";
import { apiClient } from "@/lib/api";
import { transformSettings } from "@/lib/transforms/settings";

export function useSettings(swipeNavigation: boolean = false) {
  const queryClient = useQueryClient();

  const panelQuery = useQuery({
    queryKey: ["settings", "panel"],
    queryFn: () => apiClient.panelSettings(),
  });

  const appearanceQuery = useQuery({
    queryKey: ["settings", "appearance"],
    queryFn: () => apiClient.appearanceSettings(),
  });

  const settings: Pick<
    SettingsPageProps,
    "panelSettings" | "appearanceSettings"
  > | undefined =
    panelQuery.data && appearanceQuery.data
      ? transformSettings(panelQuery.data, appearanceQuery.data, swipeNavigation)
      : undefined;

  const saveAppearance = useMutation({
    mutationFn: (payload: {
      theme: "system" | "light" | "dark";
      density: "comfortable" | "compact";
      help_mode: "off" | "basic" | "full";
    }) => apiClient.updateAppearanceSettings(payload),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["settings", "appearance"] });
    },
  });

  const savePanelSettings = useMutation({
    mutationFn: (payload: {
      http_public_url: string;
      grpc_public_endpoint: string;
    }) => apiClient.updatePanelSettings(payload),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["settings", "panel"] });
    },
  });

  const isLoading = panelQuery.isLoading || appearanceQuery.isLoading;
  const error = panelQuery.error ?? appearanceQuery.error;

  return {
    settings,
    isLoading,
    error,
    saveAppearance,
    savePanelSettings,
  };
}
