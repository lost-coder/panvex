import { SettingsPage } from "./SettingsPage";
import { useNavigate } from "@tanstack/react-router";
import { useSettings } from "./hooks/useSettings";
import { useProfile } from "@/features/auth/hooks/useProfile";
import { useRetentionSettings } from "./hooks/useRetentionSettings";
import { useAppearance } from "@/app/providers/AppearanceProvider";
import { ErrorState } from "@/components/ErrorState";
import { SkeletonRows } from "@/ui";
import { UpdatesSettingsSection } from "./UpdatesSettingsSection";
import { GeoIPSettingsSection } from "./GeoIPSettingsSection";
import { useSettingsRegistry } from "./registry";

export function SettingsContainer() {
  const navigate = useNavigate();
  const { swipeNavigation, setSwipeNavigation } = useAppearance();
  const { settings, isLoading, error, saveAppearance, savePanelSettings } = useSettings(swipeNavigation);
  const { profile } = useProfile();
  const { retention, save: saveRetention } = useRetentionSettings();
  const isAdmin = profile?.role === "admin";

  const reg = useSettingsRegistry();

  if (isLoading || !settings) {
    return (
      <div className="px-4 md:px-8 py-8">
        <SkeletonRows count={5} label="Загрузка настроек…" />
      </div>
    );
  }

  if (error) {
    return <ErrorState description={error.message} onRetry={() => globalThis.location.reload()} />;
  }

  return (
    <SettingsPage
      panelSettings={settings.panelSettings}
      appearanceSettings={settings.appearanceSettings}
      onPanelSettingsChange={(s) => savePanelSettings.mutate({
        http_public_url: s.httpPublicUrl,
        grpc_public_endpoint: s.grpcPublicEndpoint,
        password_min_length: s.passwordMinLength,
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
      retentionSaving={saveRetention.isPending}
      registry={{
        schema: reg.schema,
        values: reg.values,
        bootstrapNames: reg.bootstrapNames,
        pendingRestart: reg.pendingRestart,
        draft: reg.draft,
        isDirty: reg.isDirty,
        errors: reg.errors,
        isSaving: reg.isSaving,
        isRestartInFlight: reg.isRestartInFlight,
        onDraftChange: reg.setDraft,
        onSave: reg.save,
        onCancelDraft: reg.resetDraft,
        onRestart: reg.restart,
      }}
    >
      {isAdmin && <UpdatesSettingsSection />}
      {isAdmin && <GeoIPSettingsSection />}
    </SettingsPage>
  );
}
