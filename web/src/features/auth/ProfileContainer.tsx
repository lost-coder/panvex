import { useState } from "react";
import { useTranslation } from "react-i18next";
import { SkeletonRows } from "@/ui";
import { ProfilePage } from "./ProfilePage";
import { useProfile } from "./hooks/useProfile";
import { useSettings } from "@/features/settings/hooks/useSettings";
import { useProfileTotp } from "./hooks/useProfileTotp";
import {
  DEFAULT_LANGUAGE,
  SUPPORTED_LANGUAGES,
  setLanguage,
  type SupportedLanguage,
} from "@/shared/lib/i18n";

export function ProfileContainer() {
  const { profile, isLoading: profileLoading } = useProfile();
  const { settings, isLoading: settingsLoading, saveAppearance } = useSettings();
  const { setupMutation, enableMutation, disableMutation } = useProfileTotp();
  const [totpError, setTotpError] = useState<string | undefined>();
  const { i18n } = useTranslation();
  const currentLanguage: SupportedLanguage =
    (SUPPORTED_LANGUAGES as readonly string[]).includes(i18n.language)
      ? (i18n.language as SupportedLanguage)
      : DEFAULT_LANGUAGE;

  if (profileLoading || settingsLoading || !profile || !settings) {
    return (
      <div className="px-4 md:px-8 py-8">
        <SkeletonRows count={4} />
      </div>
    );
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
      language={currentLanguage}
      onLanguageChange={(lng) => {
        void setLanguage(lng);
      }}
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
      totpSetupLoading={setupMutation.isPending}
      totpEnableLoading={enableMutation.isPending}
      totpDisableLoading={disableMutation.isPending}
      totpError={totpError}
    />
  );
}
