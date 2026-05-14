import type { SupportedLanguage } from "@/shared/lib/i18n";

import type { SettingsPageProps } from "./settings";

// --- Profile ---

export interface TotpSetupData {
  secret: string;
  otpauthUrl: string;
}

export interface ProfilePageProps {
  user: {
    id: string;
    username: string;
    role: string;
    totpEnabled: boolean;
  };
  appearance: SettingsPageProps["appearanceSettings"];
  onAppearanceChange?: ((settings: SettingsPageProps["appearanceSettings"]) => void) | undefined;
  language: SupportedLanguage;
  onLanguageChange?: ((lng: SupportedLanguage) => void) | undefined;
  onStartTotpSetup?: (() => Promise<TotpSetupData>) | undefined;
  onEnableTotp?: ((password: string, totpCode: string) => Promise<void>) | undefined;
  onDisableTotp?: ((password: string, totpCode: string) => Promise<void>) | undefined;
  totpSetupLoading?: boolean | undefined;
  totpEnableLoading?: boolean | undefined;
  totpDisableLoading?: boolean | undefined;
  totpError?: string | undefined;
}
