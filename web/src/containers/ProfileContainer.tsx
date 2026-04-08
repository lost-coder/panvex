import { useState } from "react";
import { ProfilePage, Spinner } from "@panvex/ui";
import { useProfile } from "@/hooks/useProfile";
import { useSettings } from "@/hooks/useSettings";
import { useProfileTotp } from "@/hooks/useProfileTotp";

export function ProfileContainer() {
  const { profile, isLoading: profileLoading } = useProfile();
  const { settings, isLoading: settingsLoading, saveAppearance } = useSettings();
  const { setupMutation, enableMutation, disableMutation } = useProfileTotp();
  const [totpError, setTotpError] = useState<string | undefined>();

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
      onStartTotpSetup={async () => {
        setTotpError(undefined);
        const result = await setupMutation.mutateAsync();
        return { secret: result.secret, otpauthUrl: result.otpauth_url };
      }}
      onEnableTotp={async (password, totpCode) => {
        setTotpError(undefined);
        try {
          await enableMutation.mutateAsync({ password, totp_code: totpCode });
        } catch (err) {
          setTotpError(err instanceof Error ? err.message : "Failed to enable TOTP");
          throw err;
        }
      }}
      onDisableTotp={async (password, totpCode) => {
        setTotpError(undefined);
        try {
          await disableMutation.mutateAsync({ password, totp_code: totpCode });
        } catch (err) {
          setTotpError(err instanceof Error ? err.message : "Failed to disable TOTP");
          throw err;
        }
      }}
      totpEnableLoading={enableMutation.isPending}
      totpDisableLoading={disableMutation.isPending}
      totpError={totpError}
    />
  );
}
