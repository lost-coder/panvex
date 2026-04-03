import { ProfilePage, Spinner } from "@panvex/ui";
import { useProfile } from "@/hooks/useProfile";
import { useSettings } from "@/hooks/useSettings";

export function ProfileContainer() {
  const { profile, isLoading: profileLoading } = useProfile();
  const { settings, isLoading: settingsLoading, saveAppearance } = useSettings();

  if (profileLoading || settingsLoading || !profile || !settings) {
    return <div className="flex items-center justify-center h-64"><Spinner /></div>;
  }

  return (
    <ProfilePage
      user={{
        id: profile.id,
        username: profile.username,
        role: profile.role,
        totpEnabled: profile.totp_enabled,
      }}
      appearance={settings.appearanceSettings}
      onAppearanceChange={(s) => saveAppearance.mutate({
        theme: s.theme,
        density: s.density,
        help_mode: s.helpMode,
      })}
    />
  );
}
