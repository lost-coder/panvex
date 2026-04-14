import { SettingsPage, Spinner } from "@panvex/ui";
import { useNavigate } from "@tanstack/react-router";
import { useSettings } from "@/hooks/useSettings";
import { useProfile } from "@/hooks/useProfile";
import { useRetentionSettings } from "@/hooks/useRetentionSettings";
import { useAppearance } from "@/providers/AppearanceProvider";
import { ErrorState } from "@/components/ErrorState";
import { UpdatesSettingsSection } from "./UpdatesSettingsSection";

export function SettingsContainer() {
  const navigate = useNavigate();
  const { swipeNavigation, setSwipeNavigation } = useAppearance();
  const { settings, isLoading, error, saveAppearance, savePanelSettings } = useSettings(swipeNavigation);
  const { profile } = useProfile();
  const { retention, save: saveRetention } = useRetentionSettings();
  const isAdmin = profile?.role === "admin";

  if (isLoading || !settings) {
    return <div className="flex items-center justify-center h-64"><Spinner /></div>;
  }

  if (error) {
    return <ErrorState message={error.message} onRetry={() => window.location.reload()} />;
  }

  return (
    <>
      <SettingsPage
        panelSettings={settings.panelSettings}
        appearanceSettings={settings.appearanceSettings}
        onPanelSettingsChange={(s) => savePanelSettings.mutate({
          http_public_url: s.httpPublicUrl,
          grpc_public_endpoint: s.grpcPublicEndpoint,
        })}
        onAppearanceChange={(s) => {
          setSwipeNavigation(s.swipeNavigation);
          saveAppearance.mutate({
            theme: s.theme,
            density: s.density,
            help_mode: s.helpMode,
          });
        }}
        onManageUsers={isAdmin ? () => navigate({ to: "/settings/users" }) : undefined}
        retentionSettings={isAdmin && retention ? retention : undefined}
        onRetentionChange={isAdmin ? (s) => saveRetention.mutate(s) : undefined}
      />
      {isAdmin && <UpdatesSettingsSection />}
    </>
  );
}
