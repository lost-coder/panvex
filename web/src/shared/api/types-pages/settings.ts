// --- Settings ---

export interface SettingsPageProps {
  panelSettings: {
    httpPublicUrl: string;
    grpcPublicEndpoint: string;
  };
  appearanceSettings: {
    theme: "system" | "light" | "dark";
    density: "comfortable" | "compact";
    helpMode: "off" | "basic" | "full";
    swipeNavigation: boolean;
  };
  onPanelSettingsChange?: ((settings: SettingsPageProps["panelSettings"]) => void) | undefined;
  onAppearanceChange?: ((settings: SettingsPageProps["appearanceSettings"]) => void) | undefined;
  onRestart?: (() => void) | undefined;
  onManageUsers?: (() => void) | undefined;
  retentionSettings?: {
    ts_raw_seconds: number;
    ts_hourly_seconds: number;
    ts_dc_seconds: number;
    ip_history_seconds: number;
    event_history_seconds: number;
  } | undefined;
  onRetentionChange?: ((settings: NonNullable<SettingsPageProps["retentionSettings"]>) => void) | undefined;
  /** True while the retention save mutation is in-flight. Disables the Save
   *  button and swaps the label to "Saving…" so the operator sees feedback. */
  retentionSaving?: boolean | undefined;
  /** Content injected into the "Updates" tab (e.g. UpdatesSettingsSection from core/web). */
  children?: React.ReactNode | undefined;
}
