// --- Settings ---

import type { SchemaEntry, ValuesEntry } from "@/features/settings/registry/types";

/** Registry bundle passed from SettingsContainer to SettingsPage. */
export interface SettingsRegistryProps {
  schema: SchemaEntry[];
  values: Record<string, ValuesEntry>;
  bootstrapNames: Set<string>;
  pendingRestart: string[];
  draft: Record<string, string>;
  isDirty: boolean;
  errors: Record<string, string>;
  isSaving: boolean;
  isRestartInFlight: boolean;
  onDraftChange: (name: string, value: string) => void;
  onSave: () => Promise<void>;
  onCancelDraft: () => void;
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  onRestart: () => Promise<any>;
}

export interface SettingsPageProps {
  panelSettings: {
    httpPublicUrl: string;
    grpcPublicEndpoint: string;
    passwordMinLength: number;
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
  /** Registry props — schema-driven sections + restart banner. Optional so existing
   *  tests and usage without the registry hook continue to work. */
  registry?: SettingsRegistryProps | undefined;
}
