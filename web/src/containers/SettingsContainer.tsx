import { SettingsPage, Spinner } from "@panvex/ui";
import { useSettings } from "@/hooks/useSettings";

export function SettingsContainer() {
  const { settings, isLoading, saveAppearance, savePanelSettings } = useSettings();

  if (isLoading || !settings) {
    return <div className="flex items-center justify-center h-64"><Spinner /></div>;
  }

  return (
    <SettingsPage
      panelSettings={settings.panelSettings}
      appearanceSettings={settings.appearanceSettings}
      onPanelSettingsChange={(s) => savePanelSettings.mutate({
        http_public_url: s.httpPublicUrl,
        grpc_public_endpoint: s.grpcPublicEndpoint,
      })}
      onAppearanceChange={(s) => saveAppearance.mutate({
        theme: s.theme,
        density: s.density,
        help_mode: s.helpMode,
      })}
    />
  );
}
